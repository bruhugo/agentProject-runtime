package types

type TierType string

const (
	FREE  TierType = "free"
	BASIC TierType = "basic"
	PRO   TierType = "pro"
)

type Features struct {
	CustomSkills    bool `json:"custom_skills"`
	Cron            bool `json:"cron"`
	Email           bool `json:"email"`
	Telegram        bool `json:"telegram"`
	Whatsapp        bool `json:"whatsapp"`
	ReadFileSystem  bool `json:"read_file_system"`
	WriteFileSystem bool `json:"write_file_system"`
}

type Limits struct {
	Cpu         float32 `json:"cpu"`
	MemoryMb    uint32  `json:"memory_mb"`
	WorkspaceMb uint32  `json:"workspace_mb"`
	StateMb     uint32  `json:"state_mb"`
	LogsMb      uint32  `json:"logs_mb"`
	MaxAgents   uint32  `json:"max_agents"`
	MaxSkills   uint32  `json:"max_skills"`
}

type Tier struct {
	Tier               string   `json:"tier"`
	PricingUsdPerMonth float32  `json:"pricing_usd_per_month"`
	Limits             Limits   `json:"limits"`
	Features           Features `json:"features"`
}
