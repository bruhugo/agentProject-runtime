package config

type Config struct {
	MainNodeAddress string `env:"MAIN_NODE_ADDRESS" json:"main_node_address"`
	MainNodePort    int64  `env:"MAIN_NODE_PORT" json:"main_node_port"`

	RedisHost     string `env:"REDIS_HOST" json:"redis_host"`
	RedisPort     int    `env:"REDIS_PORT" json:"redis_port"`
	RedisUser     string `env:"REDIS_USER" json:"redis_user"`
	RedisPassword string `env:"REDIS_PASSWORD" json:"redis_password"`

	S3AccessKeyID     string `env:"AWS_ACCESS_KEY_ID,required"`
	S3SecretAccessKey string `env:"AWS_SECRET_KEY,required"`
	S3Region          string `env:"AWS_REGION,required"`
	S3Bucket          string `env:"S3_BUCKET,required"`

	JwtSecret string `env:"JWT_SECRET" json:"jwt_secret"`

	PicoclawImage string `env:"PICOCLAW_IMAGE" json:"picoclaw_image"`

	SystemUser string `env:"USER"`
	ServerUser string `env:"SERVER_USER"`
	LogLevel   string `env:"LOG_LEVEL"`
}

var AppConfig Config
