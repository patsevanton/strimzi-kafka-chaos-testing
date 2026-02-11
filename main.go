package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/linkedin/goavro/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/riferrei/srclient"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/scram"
)

const (
	ModeProducer = "producer"
	ModeConsumer = "consumer"
)

// Health status for probes
var (
	isReady   atomic.Bool
	isHealthy atomic.Bool
	logger    *slog.Logger
)

type Config struct {
	Mode              string
	Brokers           []string
	Topic             string
	SchemaRegistryURL string
	Username          string
	Password          string
	GroupID           string
	// Redis: store hash of message value for delivery verification and SLO
	RedisAddr       string
	RedisPassword   string
	RedisKeyPrefix  string
	RedisSLOSeconds int // messages still in Redis older than this are counted as SLO breach
}

type Message struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Data      string    `json:"data"`
}

func init() {
	// Initialize JSON logger
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
}

func main() {
	config := loadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start health check server
	go startHealthServer()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Shutting down...")
		isHealthy.Store(false)
		isReady.Store(false)
		cancel()
	}()

	switch config.Mode {
	case ModeProducer:
		runProducer(ctx, config)
	case ModeConsumer:
		runConsumer(ctx, config)
	default:
		logger.Error("Invalid mode", "mode", config.Mode, "valid_modes", []string{ModeProducer, ModeConsumer})
		os.Exit(1)
	}
}

// startHealthServer starts HTTP server for health probes
func startHealthServer() {
	healthPort := os.Getenv("HEALTH_PORT")
	if healthPort == "" {
		healthPort = "8080"
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if isHealthy.Load() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("not healthy"))
		}
	})

	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if isReady.Load() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("not ready"))
		}
	})

	http.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		// Liveness is always ok if server is running
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Prometheus metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	logger.Info("Starting health server", "port", healthPort)
	if err := http.ListenAndServe(":"+healthPort, nil); err != nil {
		logger.Error("Health server error", "error", err)
	}
}

func loadConfig() *Config {
	mode := os.Getenv("MODE")
	if mode == "" {
		mode = ModeProducer
	}

	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "localhost:9092"
	}

	topic := os.Getenv("KAFKA_TOPIC")
	if topic == "" {
		topic = "test-topic"
	}

	schemaRegistryURL := os.Getenv("SCHEMA_REGISTRY_URL")
	if schemaRegistryURL == "" {
		schemaRegistryURL = "http://localhost:8081"
	}

	username := os.Getenv("KAFKA_USERNAME")
	password := os.Getenv("KAFKA_PASSWORD")

	groupID := os.Getenv("KAFKA_GROUP_ID")
	if groupID == "" {
		groupID = "test-group"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisKeyPrefix := os.Getenv("REDIS_KEY_PREFIX")
	if redisKeyPrefix == "" {
		redisKeyPrefix = "kafka-msg:"
	}
	redisSLOSeconds := 60
	if s := os.Getenv("REDIS_SLO_SECONDS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			redisSLOSeconds = n
		}
	}

	return &Config{
		Mode:              mode,
		Brokers:           parseBrokers(brokers),
		Topic:             topic,
		SchemaRegistryURL: schemaRegistryURL,
		Username:          username,
		Password:          password,
		GroupID:           groupID,
		RedisAddr:         redisAddr,
		RedisPassword:     redisPassword,
		RedisKeyPrefix:    redisKeyPrefix,
		RedisSLOSeconds:   redisSLOSeconds,
	}
}

func parseBrokers(brokers string) []string {
	if brokers == "" {
		return []string{"localhost:9092"}
	}
	// Split by comma and trim whitespace
	parts := strings.Split(brokers, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return []string{"localhost:9092"}
	}
	return result
}

// hashValue returns SHA256 hex of data (used to verify message integrity via Redis).
func hashValue(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

const (
	redisKeySentTotal    = "metrics:sent_total"
	redisKeyReceivedTotal = "metrics:received_total"
)

func newRedisClient(cfg *Config) *redis.Client {
	if cfg.RedisAddr == "" {
		return nil
	}
	opts := &redis.Options{Addr: cfg.RedisAddr}
	if cfg.RedisPassword != "" {
		opts.Password = cfg.RedisPassword
	}
	return redis.NewClient(opts)
}

// redisMsgKey returns Redis key for a message: prefix + Kafka message key.
func redisMsgKey(prefix, kafkaKey string) string {
	return prefix + kafkaKey
}

func runProducer(ctx context.Context, config *Config) {
	logger.Info("Starting producer", "brokers", config.Brokers, "topic", config.Topic)

	// Mark as healthy (process is running)
	isHealthy.Store(true)

	// Create writer with simplified configuration
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(config.Brokers...),
		Topic:                  config.Topic,
		Balancer:               &kafka.LeastBytes{},
		RequiredAcks:           kafka.RequireAll,
		AllowAutoTopicCreation: true,
	}

	// Add SASL/SCRAM authentication if credentials provided
	if config.Username != "" && config.Password != "" {
		mechanism, err := scram.Mechanism(scram.SHA512, config.Username, config.Password)
		if err != nil {
			logger.Error("Failed to create SCRAM mechanism", "error", err)
			os.Exit(1)
		}
		writer.Transport = &kafka.Transport{
			SASL: mechanism,
		}
	}
	defer writer.Close()

	// Setup Schema Registry client
	schemaRegistryClient := srclient.CreateSchemaRegistryClient(config.SchemaRegistryURL)
	// Schema Registry (Karapace) may take some time to respond after rollout/port-forward.
	// Bump HTTP timeout to avoid flaky startup failures.
	schemaRegistryClient.SetTimeout(2 * time.Minute)

	// Get or create Avro schema
	schema, err := getOrCreateSchema(schemaRegistryClient, config.Topic)
	if err != nil {
		logger.Error("Failed to get/create schema", "error", err)
		os.Exit(1)
	}

	codec, err := goavro.NewCodec(schema.Schema())
	if err != nil {
		logger.Error("Failed to create Avro codec", "error", err)
		os.Exit(1)
	}

	// Wait for metadata to be fetched
	logger.Info("Waiting for Kafka metadata...")
	time.Sleep(5 * time.Second)

	// Mark connection as connected
	for _, broker := range config.Brokers {
		kafkaConnectionStatus.WithLabelValues(broker).Set(1)
	}
	schemaRegistryConnectionStatus.Set(1)

	// Redis client for delivery verification (hash + SLO)
	rdb := newRedisClient(config)
	if rdb != nil {
		defer rdb.Close()
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Warn("Redis ping failed, hash storage disabled", "error", err)
			rdb = nil
		} else {
			logger.Info("Redis connected for hash storage")
		}
	}

	// Mark as ready (connected to Kafka and Schema Registry)
	isReady.Store(true)
	logger.Info("Producer is ready")

	messageID := int64(0)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Producer stopped")
			return
		case <-ticker.C:
			messageID++
			msgStartTime := time.Now()
			msg := Message{
				ID:        messageID,
				Timestamp: time.Now(),
				Data:      fmt.Sprintf("Test message #%d", messageID),
			}

			// Convert message to Avro with Confluent wire format
			encodeStart := time.Now()
			avroData, err := encodeAvroMessage(codec, schema.ID(), msg)
			encodeDuration := time.Since(encodeStart).Seconds()
			producerMessageEncodeDuration.WithLabelValues(config.Topic).Observe(encodeDuration)

			if err != nil {
				logger.Error("Failed to encode message", "error", err, "message_id", messageID)
				producerErrorsTotal.WithLabelValues(config.Topic, "encode").Inc()
				continue
			}

			// Prepare Kafka message (schema ID is now embedded in the value)
			kafkaKey := fmt.Sprintf("key-%d", messageID)
			kafkaMsg := kafka.Message{
				Key:   []byte(kafkaKey),
				Value: avroData,
			}

			err = writer.WriteMessages(ctx, kafkaMsg)
			totalDuration := time.Since(msgStartTime).Seconds()

			if err != nil {
				logger.Error("Failed to write message", "error", err, "message_id", messageID)
				producerErrorsTotal.WithLabelValues(config.Topic, "send").Inc()
				// Mark connection as disconnected on error
				for _, broker := range config.Brokers {
					kafkaConnectionStatus.WithLabelValues(broker).Set(0)
				}
				continue
			}

			// Store hash in Redis: key = same as Kafka key, value = hash:timestamp_ms (for SLO)
			if rdb != nil {
				msgHash := hashValue(avroData)
				redisKey := redisMsgKey(config.RedisKeyPrefix, kafkaKey)
				redisVal := msgHash + ":" + strconv.FormatInt(time.Now().UnixMilli(), 10)
				if err := rdb.Set(ctx, redisKey, redisVal, 0).Err(); err != nil {
					logger.Warn("Redis SET failed", "key", redisKey, "error", err)
				}
				if err := rdb.Incr(ctx, redisKeySentTotal).Err(); err != nil {
					logger.Warn("Redis INCR sent_total failed", "error", err)
				}
			}

			// Update metrics
			producerMessagesSentTotal.WithLabelValues(config.Topic).Inc()
			producerMessagesSentBytes.WithLabelValues(config.Topic).Add(float64(len(avroData)))
			producerMessageSendDuration.WithLabelValues(config.Topic).Observe(totalDuration)

			logger.Info("Sent message", "message_id", messageID)
		}
	}
}

func runConsumer(ctx context.Context, config *Config) {
	logger.Info("Starting consumer", "brokers", config.Brokers, "topic", config.Topic, "group_id", config.GroupID)

	// Mark as healthy (process is running)
	isHealthy.Store(true)

	// Setup Kafka dialer
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	// Add SASL/SCRAM authentication if credentials provided
	if config.Username != "" && config.Password != "" {
		mechanism, err := scram.Mechanism(scram.SHA512, config.Username, config.Password)
		if err != nil {
			logger.Error("Failed to create SCRAM mechanism", "error", err)
			os.Exit(1)
		}
		dialer.SASLMechanism = mechanism
	}

	// Setup Kafka reader
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  config.Brokers,
		Topic:    config.Topic,
		GroupID:  config.GroupID,
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
		Dialer:   dialer,
	})
	defer reader.Close()

	// Setup Schema Registry client
	schemaRegistryClient := srclient.CreateSchemaRegistryClient(config.SchemaRegistryURL)
	// See producer: avoid flaky startup failures when SR is still warming up.
	schemaRegistryClient.SetTimeout(2 * time.Minute)

	// Mark connection as connected
	for _, broker := range config.Brokers {
		kafkaConnectionStatus.WithLabelValues(broker).Set(1)
	}
	schemaRegistryConnectionStatus.Set(1)

	// Setup Admin client for lag metrics
	transport := &kafka.Transport{
		SASL: dialer.SASLMechanism,
	}
	adminClient := &kafka.Client{
		Addr:      kafka.TCP(config.Brokers...),
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	// Redis client for delivery verification and SLO
	rdb := newRedisClient(config)
	if rdb != nil {
		defer rdb.Close()
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Warn("Redis ping failed, delivery verification disabled", "error", err)
			rdb = nil
		} else {
			logger.Info("Redis connected for delivery verification")
		}
	}

	// Start lag metrics updater in background
	go updateConsumerLag(ctx, adminClient, dialer, config)

	// Start Redis SLO metrics updater (pending count and old-pending count)
	if rdb != nil && config.RedisSLOSeconds > 0 {
		go updateRedisSLOMetrics(ctx, rdb, config)
	}

	// Mark as ready (connected to Kafka and Schema Registry)
	isReady.Store(true)
	logger.Info("Consumer is ready")

	for {
		select {
		case <-ctx.Done():
			logger.Info("Consumer stopped")
			return
		default:
			readStart := time.Now()
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				logger.Error("Error reading message", "error", err)
				consumerErrorsTotal.WithLabelValues(config.Topic, "read").Inc()
				// Mark connection as disconnected on error
				for _, broker := range config.Brokers {
					kafkaConnectionStatus.WithLabelValues(broker).Set(0)
				}
				time.Sleep(1 * time.Second)
				continue
			}

			partitionStr := fmt.Sprintf("%d", msg.Partition)

			// Decode message using Confluent wire format
			decodeStart := time.Now()
			decoded, err := decodeAvroMessage(schemaRegistryClient, msg.Value)
			decodeDuration := time.Since(decodeStart).Seconds()
			consumerMessageDecodeDuration.WithLabelValues(config.Topic, partitionStr).Observe(decodeDuration)

			if err != nil {
				logger.Error("Failed to decode message", "error", err)
				consumerErrorsTotal.WithLabelValues(config.Topic, "decode").Inc()
				continue
			}

			processingDuration := time.Since(readStart).Seconds()
			consumerMessageProcessingDuration.WithLabelValues(config.Topic, partitionStr).Observe(processingDuration)

			// Delivery verification via Redis: compare hash of value with stored hash, then DEL and INCR
			if rdb != nil {
				redisKey := redisMsgKey(config.RedisKeyPrefix, string(msg.Key))
				stored, err := rdb.Get(ctx, redisKey).Result()
				if err == nil {
					parts := strings.SplitN(stored, ":", 2)
					expectedHash := parts[0]
					gotHash := hashValue(msg.Value)
					if gotHash == expectedHash {
						if err := rdb.Del(ctx, redisKey).Err(); err != nil {
							logger.Warn("Redis DEL failed", "key", redisKey, "error", err)
						}
						if err := rdb.Incr(ctx, redisKeyReceivedTotal).Err(); err != nil {
							logger.Warn("Redis INCR received_total failed", "error", err)
						}
					} else {
						logger.Warn("Hash mismatch", "key", redisKey, "expected", expectedHash, "got", gotHash)
						consumerRedisHashMismatchTotal.WithLabelValues(config.Topic, partitionStr).Inc()
					}
				} else if err != redis.Nil {
					logger.Warn("Redis GET failed", "key", redisKey, "error", err)
				}
				// If redis.Nil: key not in Redis (e.g. producer didn't use Redis or already deleted)
			}

			// Calculate end-to-end latency if message has timestamp
			if decodedMap, ok := decoded.(map[string]interface{}); ok {
				if timestampVal, ok := decodedMap["timestamp"]; ok {
					var msgTimestamp time.Time
					switch v := timestampVal.(type) {
					case time.Time:
						// goavro may decode timestamp-millis logicalType as time.Time
						msgTimestamp = v
					case int64:
						msgTimestamp = time.UnixMilli(v)
					case int32:
						msgTimestamp = time.UnixMilli(int64(v))
					case int:
						msgTimestamp = time.UnixMilli(int64(v))
					case float64:
						msgTimestamp = time.UnixMilli(int64(v))
					case float32:
						msgTimestamp = time.UnixMilli(int64(v))
					default:
						logger.Debug("Timestamp has unsupported type", "type", fmt.Sprintf("%T", v), "value", v)
						// Try to continue without end-to-end latency metric
					}
					if !msgTimestamp.IsZero() {
						endToEndLatency := time.Since(msgTimestamp).Seconds()
						consumerEndToEndLatency.WithLabelValues(config.Topic, partitionStr).Observe(endToEndLatency)
					}
				}
			}

			// Update metrics
			consumerMessagesReceivedTotal.WithLabelValues(config.Topic, partitionStr).Inc()
			consumerMessagesReceivedBytes.WithLabelValues(config.Topic, partitionStr).Add(float64(len(msg.Value)))

			logger.Info("Received message", "key", string(msg.Key), "value", decoded, "partition", msg.Partition, "offset", msg.Offset)
		}
	}
}

// updateRedisSLOMetrics periodically counts pending messages in Redis and those older than SLO threshold.
func updateRedisSLOMetrics(ctx context.Context, rdb *redis.Client, config *Config) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	sloThreshold := time.Duration(config.RedisSLOSeconds) * time.Second
	prefix := config.RedisKeyPrefix

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var pending, oldPending int
			iter := rdb.Scan(ctx, 0, prefix+"*", 100).Iterator()
			for iter.Next(ctx) {
				key := iter.Val()
				if key == redisKeySentTotal || key == redisKeyReceivedTotal {
					continue
				}
				pending++
				val, err := rdb.Get(ctx, key).Result()
				if err != nil {
					continue
				}
				parts := strings.SplitN(val, ":", 2)
				if len(parts) != 2 {
					continue
				}
				tsMs, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					continue
				}
				if time.Since(time.UnixMilli(tsMs)) > sloThreshold {
					oldPending++
				}
			}
			if err := iter.Err(); err != nil {
				logger.Debug("Redis SCAN error", "error", err)
				continue
			}
			redisPendingMessages.Set(float64(pending))
			redisPendingOldMessages.Set(float64(oldPending))
		}
	}
}

// updateConsumerLag periodically updates consumer lag metrics
func updateConsumerLag(ctx context.Context, adminClient *kafka.Client, dialer *kafka.Dialer, config *Config) {
	ticker := time.NewTicker(30 * time.Second) // Update every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Connect to first broker to get partition info
			conn, err := dialer.DialContext(ctx, "tcp", config.Brokers[0])
			if err != nil {
				logger.Debug("Failed to dial broker for lag metrics", "error", err)
				continue
			}

			// Get all partitions for the topic
			partitions, err := conn.ReadPartitions(config.Topic)
			if err != nil {
				logger.Debug("Failed to read partitions", "error", err)
				conn.Close()
				continue
			}

			// Build map of partitions for Admin API
			partitionIDs := make([]int, 0, len(partitions))
			for _, p := range partitions {
				partitionIDs = append(partitionIDs, p.ID)
			}

			// Get consumer group committed offsets using OffsetFetch API
			offsetFetchResp, err := adminClient.OffsetFetch(ctx, &kafka.OffsetFetchRequest{
				GroupID: config.GroupID,
				Topics: map[string][]int{
					config.Topic: partitionIDs,
				},
			})
			if err != nil {
				logger.Debug("Failed to get consumer group offsets", "error", err)
				conn.Close()
				continue
			}

			// Build OffsetRequests for high watermark (LastOffset per partition)
			offsetReqs := make([]kafka.OffsetRequest, len(partitionIDs))
			for i, id := range partitionIDs {
				offsetReqs[i] = kafka.LastOffsetOf(id)
			}
			listOffsetsResp, err := adminClient.ListOffsets(ctx, &kafka.ListOffsetsRequest{
				Topics: map[string][]kafka.OffsetRequest{
					config.Topic: offsetReqs,
				},
			})
			if err != nil {
				logger.Debug("Failed to get partition offsets", "error", err)
				conn.Close()
				continue
			}

			// For each partition, calculate lag
			for _, partition := range partitions {
				// Get high watermark from ListOffsets response
				highWatermark := int64(-1)
				if topicOffsets, ok := listOffsetsResp.Topics[config.Topic]; ok {
					for _, po := range topicOffsets {
						if po.Partition == partition.ID {
							highWatermark = po.LastOffset
							break
						}
					}
				}
				if highWatermark < 0 {
					logger.Debug("Failed to get high watermark", "partition", partition.ID)
					continue
				}

				// Get consumer group committed offset from OffsetFetch response
				consumerOffset := int64(-1)
				if topicParts, ok := offsetFetchResp.Topics[config.Topic]; ok {
					for _, p := range topicParts {
						if p.Partition == partition.ID {
							consumerOffset = p.CommittedOffset
							break
						}
					}
				}

				// Calculate lag: highWatermark - consumerOffset
				if consumerOffset >= 0 {
					lag := highWatermark - consumerOffset
					if lag < 0 {
						lag = 0
					}
					partitionStr := fmt.Sprintf("%d", partition.ID)
					consumerLag.WithLabelValues(config.Topic, partitionStr, config.GroupID).Set(float64(lag))
				}
			}

			conn.Close()
		}
	}
}

func getOrCreateSchema(client *srclient.SchemaRegistryClient, subject string) (*srclient.Schema, error) {
	// Try to get latest schema first
	start := time.Now()
	schema, err := client.GetLatestSchema(subject)
	duration := time.Since(start).Seconds()
	schemaRegistryRequestDuration.WithLabelValues("get_latest_schema").Observe(duration)
	schemaRegistryRequestsTotal.WithLabelValues("get_latest_schema").Inc()

	if err == nil {
		return schema, nil
	}

	schemaRegistryErrorsTotal.WithLabelValues("get_latest_schema", "not_found").Inc()

	// If not found, create new schema
	avroSchema := `{
		"type": "record",
		"name": "Message",
		"namespace": "com.example",
		"fields": [
			{"name": "id", "type": "long"},
			{"name": "timestamp", "type": "long", "logicalType": "timestamp-millis"},
			{"name": "data", "type": "string"}
		]
	}`

	start = time.Now()
	schema, err = client.CreateSchema(subject, avroSchema, srclient.Avro)
	duration = time.Since(start).Seconds()
	schemaRegistryRequestDuration.WithLabelValues("create_schema").Observe(duration)
	schemaRegistryRequestsTotal.WithLabelValues("create_schema").Inc()

	if err != nil {
		schemaRegistryErrorsTotal.WithLabelValues("create_schema", "invalid_schema").Inc()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return schema, nil
}

func decodeAvroMessage(client *srclient.SchemaRegistryClient, data []byte) (interface{}, error) {
	// Confluent wire format: magic byte (0) + schema ID (4 bytes big-endian) + Avro data
	if len(data) < 5 {
		return nil, fmt.Errorf("message too short: %d bytes", len(data))
	}

	if data[0] != 0 {
		return nil, fmt.Errorf("invalid magic byte: %d", data[0])
	}

	// Extract schema ID (big-endian)
	schemaID := int(data[1])<<24 | int(data[2])<<16 | int(data[3])<<8 | int(data[4])

	// Get schema from Schema Registry
	start := time.Now()
	schema, err := client.GetSchema(schemaID)
	duration := time.Since(start).Seconds()
	schemaRegistryRequestDuration.WithLabelValues("get_schema").Observe(duration)
	schemaRegistryRequestsTotal.WithLabelValues("get_schema").Inc()

	if err != nil {
		schemaRegistryErrorsTotal.WithLabelValues("get_schema", "not_found").Inc()
		return nil, fmt.Errorf("failed to get schema %d: %w", schemaID, err)
	}

	// Create codec and decode
	codec, err := goavro.NewCodec(schema.Schema())
	if err != nil {
		return nil, fmt.Errorf("failed to create codec: %w", err)
	}

	decoded, _, err := codec.NativeFromBinary(data[5:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode Avro: %w", err)
	}

	return decoded, nil
}

func encodeAvroMessage(codec *goavro.Codec, schemaID int, msg Message) ([]byte, error) {
	// Convert Message to map for Avro encoding
	avroMap := map[string]interface{}{
		"id":        msg.ID,
		"timestamp": msg.Timestamp.UnixMilli(),
		"data":      msg.Data,
	}

	// Encode to Avro binary
	avroData, err := codec.BinaryFromNative(nil, avroMap)
	if err != nil {
		return nil, err
	}

	// Use Confluent wire format: magic byte (0) + schema ID (4 bytes big-endian) + Avro data
	// This is the standard format expected by Schema Registry consumers
	buf := make([]byte, 5+len(avroData))
	buf[0] = 0 // Magic byte
	buf[1] = byte(schemaID >> 24)
	buf[2] = byte(schemaID >> 16)
	buf[3] = byte(schemaID >> 8)
	buf[4] = byte(schemaID)
	copy(buf[5:], avroData)

	return buf, nil
}
