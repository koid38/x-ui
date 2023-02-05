package model

type TgClient struct {
	ChatID   int64  `json:"chatId" form:"chatId" gorm:"primaryKey"`
	Name     string `json:"clientName" form:"clientName"`
	Email    string `json:"clientEmail" form:"clientEmail"`
	Uid      string `json:"clientUid" form:"clientUid"`
	Approved bool   `json:"approved" form:"approved"`
}
