package config

import (
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all service configuration loaded from environment variables.
type Config struct {
	AppEnv      string `env:"APP_ENV" envDefault:"local"`
	HTTPAddr    string `env:"HTTP_ADDR" envDefault:":8080"`
	GRPCAddr    string `env:"GRPC_ADDR" envDefault:":9090"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
	MetricsAddr string `env:"METRICS_ADDR" envDefault:":9100"`

	JWTIssuer      string   `env:"JWT_ISSUER" envDefault:"dispatch-auth"`
	JWTAudience    string   `env:"JWT_AUDIENCE" envDefault:"dispatch-engine"`
	JWKSURL        string   `env:"JWKS_URL"`
	JWTAllowedAlgs []string `env:"JWT_ALLOWED_ALGS" envDefault:"RS256,ES256"`

	RedisAddrs    []string `env:"REDIS_ADDRS" envDefault:"localhost:6379"`
	RedisUsername string   `env:"REDIS_USERNAME"`
	RedisPassword string   `env:"REDIS_PASSWORD"`
	RedisTLS      bool     `env:"REDIS_TLS" envDefault:"false"`

	MongoURI string `env:"MONGO_URI" envDefault:"mongodb://localhost:27017"`
	MongoDB  string `env:"MONGO_DB" envDefault:"dispatch"`

	KafkaBrokers      []string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
	KafkaSASLUsername string   `env:"KAFKA_SASL_USERNAME"`
	KafkaSASLPassword string   `env:"KAFKA_SASL_PASSWORD"`
	KafkaTLS          bool     `env:"KAFKA_TLS" envDefault:"false"`

	FCMCredentialsFile string `env:"FCM_CREDENTIALS_FILE"`
	APNSKeyID          string `env:"APNS_KEY_ID"`
	APNSTeamID         string `env:"APNS_TEAM_ID"`
	APNSKeyFile        string `env:"APNS_KEY_FILE"`

	DispatchCandidates         int           `env:"DISPATCH_CANDIDATES" envDefault:"10"`
	DriverLockTTL              time.Duration `env:"DRIVER_LOCK_TTL" envDefault:"15s"`
	OfferTimeout               time.Duration `env:"OFFER_TIMEOUT" envDefault:"15s"`
	MaxReassignAttempts        int           `env:"MAX_REASSIGN_ATTEMPTS" envDefault:"3"`
	LocationStaleAfter         time.Duration `env:"LOCATION_STALE_AFTER" envDefault:"30s"`
	RateLimitRideRequestPerMin int           `env:"RATE_LIMIT_RIDE_REQUEST_PER_MIN" envDefault:"60"`
	OTELCollectorEndpoint      string        `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"localhost:4317"`
}

// Load parses environment variables into Config with default fallbacks.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
