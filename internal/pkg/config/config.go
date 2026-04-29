package config

import (
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

// LoadEnv loads the .env file if present (silent if missing - useful in containers).
func LoadEnv() {
	_ = godotenv.Load()
}

// Process loads env vars into the provided struct using envconfig tags.
func Process(prefix string, spec interface{}) error {
	LoadEnv()
	return envconfig.Process(prefix, spec)
}

// Common defines fields shared across services.
type Common struct {
	HTTPPort  string `envconfig:"HTTP_PORT" default:"8080"`
	GRPCPort  string `envconfig:"GRPC_PORT" default:"9000"`
	LogLevel  string `envconfig:"LOG_LEVEL" default:"info"`
	JWTSecret string `envconfig:"JWT_SECRET" default:"super-secret-change-me"`
}

// Database holds DB connection config.
type Database struct {
	Host     string `envconfig:"DB_HOST" default:"postgres"`
	Port     string `envconfig:"DB_PORT" default:"5432"`
	User     string `envconfig:"DB_USER" default:"app"`
	Password string `envconfig:"DB_PASSWORD" default:"app"`
	Name     string `envconfig:"DB_NAME" default:"app"`
	SSLMode  string `envconfig:"DB_SSLMODE" default:"disable"`
}

// DSN returns a PostgreSQL connection string.
func (d Database) DSN() string {
	return "postgres://" + d.User + ":" + d.Password + "@" + d.Host + ":" + d.Port + "/" + d.Name + "?sslmode=" + d.SSLMode
}

// RabbitMQ holds AMQP connection config.
type RabbitMQ struct {
	URL string `envconfig:"RABBITMQ_URL" default:"amqp://guest:guest@rabbitmq:5672/"`
}
