package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/linkedin/goavro/v2"
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

	return &Config{
		Mode:              mode,
		Brokers:           parseBrokers(brokers),
		Topic:             topic,
		SchemaRegistryURL: schemaRegistryURL,
		Username:          username,
		Password:          password,
		GroupID:           groupID,
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
			msg := Message{
				ID:        messageID,
				Timestamp: time.Now(),
				Data:      fmt.Sprintf("Test message #%d", messageID),
			}

			// Convert message to Avro with Confluent wire format
			avroData, err := encodeAvroMessage(codec, schema.ID(), msg)
			if err != nil {
				logger.Error("Failed to encode message", "error", err, "message_id", messageID)
				continue
			}

			// Prepare Kafka message (schema ID is now embedded in the value)
			kafkaMsg := kafka.Message{
				Key:   []byte(fmt.Sprintf("key-%d", messageID)),
				Value: avroData,
			}

			err = writer.WriteMessages(ctx, kafkaMsg)
			if err != nil {
				logger.Error("Failed to write message", "error", err, "message_id", messageID)
				continue
			}

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

	// Mark as ready (connected to Kafka and Schema Registry)
	isReady.Store(true)
	logger.Info("Consumer is ready")

	for {
		select {
		case <-ctx.Done():
			logger.Info("Consumer stopped")
			return
		default:
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				logger.Error("Error reading message", "error", err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Decode message using Confluent wire format
			decoded, err := decodeAvroMessage(schemaRegistryClient, msg.Value)
			if err != nil {
				logger.Error("Failed to decode message", "error", err)
				continue
			}

			logger.Info("Received message", "key", string(msg.Key), "value", decoded, "partition", msg.Partition, "offset", msg.Offset)
		}
	}
}

func getOrCreateSchema(client *srclient.SchemaRegistryClient, subject string) (*srclient.Schema, error) {
	// Try to get latest schema first
	schema, err := client.GetLatestSchema(subject)
	if err == nil {
		return schema, nil
	}

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

	schema, err = client.CreateSchema(subject, avroSchema, srclient.Avro)
	if err != nil {
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
	schema, err := client.GetSchema(schemaID)
	if err != nil {
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
