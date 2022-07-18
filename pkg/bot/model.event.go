package bot

type Event struct {
	ID        string `xorm:"pk"`
	GuildID   string
	ChannelID string
}
