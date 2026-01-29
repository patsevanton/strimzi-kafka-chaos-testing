package main

import (
	"context"
	"fmt"
	"log"
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
		log.Println("Shutting down...")
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
		log.Fatalf("Invalid mode: %s. Use 'producer' or 'consumer'", config.Mode)
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

	log.Printf("Starting health server on port %s", healthPort)
	if err := http.ListenAndServe(":"+healthPort, nil); err != nil {
		log.Printf("Health server error: %v", err)
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
	log.Printf("Starting producer. Brokers: %v, Topic: %s", config.Brokers, config.Topic)

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
			log.Fatalf("Failed to create SCRAM mechanism: %v", err)
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
		log.Fatalf("Failed to get/create schema: %v", err)
	}

	codec, err := goavro.NewCodec(schema.Schema())
	if err != nil {
		log.Fatalf("Failed to create Avro codec: %v", err)
	}

	// Wait for metadata to be fetched
	log.Println("Waiting for Kafka metadata...")
	time.Sleep(5 * time.Second)

	// Mark as ready (connected to Kafka and Schema Registry)
	isReady.Store(true)
	log.Println("Producer is ready")

	messageID := int64(0)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Producer stopped")
			return
		case <-ticker.C:
			messageID++
			msg := Message{
				ID:        messageID,
				Timestamp: time.Now(),
				Data:      fmt.Sprintf("Test message #%d", messageID),
			}

			// Convert message to Avro
			avroData, err := encodeAvroMessage(codec, msg)
			if err != nil {
				log.Printf("Failed to encode message: %v", err)
				continue
			}

			// Prepare Kafka message with schema ID
			kafkaMsg := kafka.Message{
				Key:   []byte(fmt.Sprintf("key-%d", messageID)),
				Value: avroData,
				Headers: []kafka.Header{
					{
						Key:   "schemaId",
						Value: []byte(fmt.Sprintf("%d", schema.ID())),
					},
				},
			}

			err = writer.WriteMessages(ctx, kafkaMsg)
			if err != nil {
				log.Printf("Failed to write message: %v", err)
				continue
			}

			log.Printf("Sent message #%d", messageID)
		}
	}
}

func runConsumer(ctx context.Context, config *Config) {
	log.Printf("Starting consumer. Brokers: %v, Topic: %s, GroupID: %s", config.Brokers, config.Topic, config.GroupID)

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
			log.Fatalf("Failed to create SCRAM mechanism: %v", err)
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
	log.Println("Consumer is ready")

	for {
		select {
		case <-ctx.Done():
			log.Println("Consumer stopped")
			return
		default:
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				log.Printf("Error reading message: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Get schema ID from headers
			var schemaID int
			for _, header := range msg.Headers {
				if header.Key == "schemaId" {
					fmt.Sscanf(string(header.Value), "%d", &schemaID)
					break
				}
			}

			// Get schema from Schema Registry
			var schema *srclient.Schema
			if schemaID > 0 {
				schema, err = schemaRegistryClient.GetSchema(schemaID)
				if err != nil {
					log.Printf("Failed to get schema %d: %v", schemaID, err)
					continue
				}
			} else {
				// Fallback: get latest schema for topic
				schema, err = schemaRegistryClient.GetLatestSchema(config.Topic)
				if err != nil {
					log.Printf("Failed to get latest schema: %v", err)
					continue
				}
			}

			// Decode Avro message
			codec, err := goavro.NewCodec(schema.Schema())
			if err != nil {
				log.Printf("Failed to create codec: %v", err)
				continue
			}

			decoded, _, err := codec.NativeFromBinary(msg.Value)
			if err != nil {
				log.Printf("Failed to decode message: %v", err)
				continue
			}

			log.Printf("Received message - Key: %s, Value: %+v", string(msg.Key), decoded)
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

func encodeAvroMessage(codec *goavro.Codec, msg Message) ([]byte, error) {
	// Convert Message to map for Avro encoding
	avroMap := map[string]interface{}{
		"id":        msg.ID,
		"timestamp": msg.Timestamp.UnixMilli(),
		"data":      msg.Data,
	}

	return codec.BinaryFromNative(nil, avroMap)
}
