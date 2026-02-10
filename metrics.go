package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Producer metrics
	producerMessagesSentTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_producer_messages_sent_total",
			Help: "Total number of messages sent by producer",
		},
		[]string{"topic"},
	)

	producerMessagesSentBytes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_producer_messages_sent_bytes_total",
			Help: "Total bytes sent by producer",
		},
		[]string{"topic"},
	)

	producerMessageSendDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_producer_message_send_duration_seconds",
			Help:    "Duration of sending a message (from creation to Kafka acknowledgment)",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		},
		[]string{"topic"},
	)

	producerMessageEncodeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_producer_message_encode_duration_seconds",
			Help:    "Duration of encoding a message to Avro format",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10), // 0.1ms to ~100ms
		},
		[]string{"topic"},
	)

	producerErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_producer_errors_total",
			Help: "Total number of producer errors",
		},
		[]string{"topic", "error_type"}, // error_type: encode, send, connection
	)

	// Consumer metrics
	consumerMessagesReceivedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_consumer_messages_received_total",
			Help: "Total number of messages received by consumer",
		},
		[]string{"topic", "partition"},
	)

	consumerMessagesReceivedBytes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_consumer_messages_received_bytes_total",
			Help: "Total bytes received by consumer",
		},
		[]string{"topic", "partition"},
	)

	consumerMessageProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_consumer_message_processing_duration_seconds",
			Help:    "Duration of processing a message (from receiving to completion)",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		},
		[]string{"topic", "partition"},
	)

	consumerMessageDecodeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_consumer_message_decode_duration_seconds",
			Help:    "Duration of decoding a message from Avro format",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10), // 0.1ms to ~100ms
		},
		[]string{"topic", "partition"},
	)

	consumerEndToEndLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_consumer_end_to_end_latency_seconds",
			Help:    "End-to-end latency from message creation (timestamp) to consumption",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms to ~40s
		},
		[]string{"topic", "partition"},
	)

	consumerErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_consumer_errors_total",
			Help: "Total number of consumer errors",
		},
		[]string{"topic", "error_type"}, // error_type: read, decode, connection
	)

	consumerLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_lag",
			Help: "Consumer lag (difference between latest offset and consumer offset)",
		},
		[]string{"topic", "partition", "group_id"},
	)

	// Schema Registry metrics
	schemaRegistryRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "schema_registry_requests_total",
			Help: "Total number of Schema Registry API requests",
		},
		[]string{"operation"}, // operation: get_schema, get_latest_schema, create_schema
	)

	schemaRegistryRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "schema_registry_request_duration_seconds",
			Help:    "Duration of Schema Registry API requests",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		},
		[]string{"operation"},
	)

	schemaRegistryErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "schema_registry_errors_total",
			Help: "Total number of Schema Registry errors",
		},
		[]string{"operation", "error_type"}, // error_type: timeout, not_found, invalid_schema, network
	)

	// Connection metrics
	kafkaConnectionStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_connection_status",
			Help: "Kafka connection status (1 = connected, 0 = disconnected)",
		},
		[]string{"broker"},
	)

	kafkaReconnectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_reconnections_total",
			Help: "Total number of Kafka reconnections",
		},
		[]string{"broker"},
	)

	schemaRegistryConnectionStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "schema_registry_connection_status",
			Help: "Schema Registry connection status (1 = connected, 0 = disconnected)",
		},
	)
)
