package model

type MsgType string

const (
	Registration MsgType = "registration"
	Renewal      MsgType = "renewal"
	FaultReport  MsgType = "fault"
	Message      MsgType = "message"
)

type TgClient struct {
	ChatID       int64         `json:"chatId" form:"chatId" gorm:"primaryKey"`
	Name         string        `json:"clientName" form:"clientName"`
	Email        string        `json:"clientEmail" form:"clientEmail"`
	Uid          string        `json:"clientUid" form:"clientUid"`
	Enabled      bool          `json:"enabled" form:"enabled"`
	TgClientMsgs []TgClientMsg `gorm:"foreignKey:ChatID;references:ChatID;constraint:OnDelete:CASCADE;"`
}

type TgClientMsg struct {
	Id     int     `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	ChatID int64   `json:"chatId" form:"chatId"`
	Type   MsgType `json:"type" form:"type"`
	Msg    string  `json:"msg" form:"msg"`
}
