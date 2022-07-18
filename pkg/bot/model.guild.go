package bot

type Guild struct {
	ID                     string `xorm:"pk"`
	NewEventChannelMessage string
}
