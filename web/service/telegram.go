package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/logger"
	"x-ui/util/common"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramService struct {
	inboundService InboundService
	settingService SettingService
}

func (j *TelegramService) GetClientUsage(id string) (string, error) {
	traffic, err := j.inboundService.GetClientTrafficById(id)
	if err != nil {
		logger.Error(err)
		return "Incorrect UUID!", err
	}
	expiryTime := ""
	if traffic.ExpiryTime == 0 {
		expiryTime = fmt.Sprintf("unlimited")
	} else {
		expiryTime = fmt.Sprintf("%s", time.Unix((traffic.ExpiryTime/1000), 0).Format("2006-01-02 15:04:05"))
	}
	total := ""
	if traffic.Total == 0 {
		total = fmt.Sprintf("unlimited")
	} else {
		total = fmt.Sprintf("%s", common.FormatTraffic((traffic.Total)))
	}
	output := fmt.Sprintf("💡 Active: %t\r\n📧 Email: %s\r\n🔼 Upload↑: %s\r\n🔽 Download↓: %s\r\n🔄 Total: %s / %s\r\n📅 Expires on: %s\r\n",
		traffic.Enable, traffic.Email, common.FormatTraffic(traffic.Up), common.FormatTraffic(traffic.Down), common.FormatTraffic((traffic.Up + traffic.Down)),
		total, expiryTime)

	return output, err
}

func (j *TelegramService) CheckIfClientExists(id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	_, err := j.inboundService.GetClientTrafficById(id)
	if err != nil {
		return false
	}
	return true
}

func (t *TelegramService) AddTgClient(client *model.TgClient) error {
	db := database.GetTgDB()
	err := db.Create(client).Error
	return err
}

func (t *TelegramService) GetTgClients() ([]*model.TgClient, error) {
	db := database.GetTgDB()
	var clients []*model.TgClient
	err := db.Model(&model.TgClient{}).Find(&clients).Error
	if err != nil {
		logger.Error(err)
		return nil, err
	}
	return clients, nil
}

func (t *TelegramService) UpdateClient(client *model.TgClient) error {

	db := database.GetTgDB()
	return db.Save(client).Error
}

func (t *TelegramService) RegisterClient(client *model.TgClient) error {

	err := t.UpdateClient(client)
	if err != nil {
		logger.Error(err)
		return err
	}

	err = t.DeleteRegRequestMsg(client.ChatID)
	if err != nil {
		logger.Error(err)
		return err
	}

	finalMsg, err := t.settingService.GetTgCrmRegFinalMsg()
	if err != nil {
		logger.Error(err)
		finalMsg = Tr("msgAccCreateSuccess")
	}
	t.SendMsgToTgbot(client.ChatID, finalMsg)
	return nil
}

func (t *TelegramService) RenewClient(client *model.TgClient) error {

	err := t.UpdateClient(client)
	if err != nil {
		logger.Error(err)
		return err
	}

	err = t.DeleteRegRequestMsg(client.ChatID)
	if err != nil {
		logger.Error(err)
		return err
	}

	finalMsg := Tr("msgRenewSuccess")

	t.SendMsgToTgbot(client.ChatID, finalMsg)
	return nil
}

func (t *TelegramService) DeleteClient(id int64) error {
	db := database.GetTgDB()
	err := db.Select("TgClientMsgs").Delete(&model.TgClient{ChatID: id}).Error
	if err != nil {
		logger.Error(err)
		return err
	}
	return nil
}

func (t *TelegramService) getTgClient(id int64) (*model.TgClient, error) {
	db := database.GetTgDB()
	client := &model.TgClient{}
	err := db.Model(&model.TgClient{}).First(&client, id).Error
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (t *TelegramService) replaceMarkup(msg *string, tgClient *model.TgClient) string {
	replacer := strings.NewReplacer("<UUID>", tgClient.Uid, "<CHAT_ID>", strconv.FormatInt(tgClient.ChatID, 10))
	return replacer.Replace(*msg)
}

func (t *TelegramService) HandleMessage(msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	if _, exists := TgSessions[msg.Chat.ID]; !exists {
		TgSessions[msg.Chat.ID] = InitFSM()
	}
	return TgSessions[msg.Chat.ID].state(TgSessions[msg.Chat.ID], msg)
}

func (t *TelegramService) CanAcceptPhoto(chatId int64) bool {
	if _, exists := TgSessions[chatId]; !exists {
		TgSessions[chatId] = InitFSM()
	}
	return TgSessions[chatId].canAcceptPhoto
}

func (t *TelegramService) SendMsgToTgbot(chatId int64, msg string) error {

	tgClient, err := t.getTgClient(chatId)
	if err == nil {
		msg = t.replaceMarkup(&msg, tgClient)
	}

	tgBottoken, err := t.settingService.GetTgBotToken()
	if err != nil || tgBottoken == "" {
		logger.Warning("sendMsgToTgbot failed,GetTgBotToken fail:", err)
		return err
	}
	bot, err := tgbotapi.NewBotAPI(tgBottoken)
	if err != nil {
		fmt.Println("get tgbot error:", err)
		return err
	}
	bot.Debug = true
	fmt.Printf("Authorized on account %s", bot.Self.UserName)
	info := tgbotapi.NewMessage(chatId, msg)
	info.ParseMode = "HTML"
	bot.Send(info)
	return nil
}

func (t *TelegramService) PushTgClientMsg(clientMsg *model.TgClientMsg) error {
	db := database.GetTgDB()
	err := db.Create(clientMsg).Error
	return err
}

func (t *TelegramService) GetTgClientMsgs() ([]*model.TgClientMsg, error) {
	db := database.GetTgDB().Model(&model.TgClientMsg{})
	var msgs []*model.TgClientMsg
	err := db.Find(&msgs).Error
	if err != nil {
		logger.Error(err)
		return nil, err
	}
	return msgs, nil
}

func (t *TelegramService) DeleteRegRequestMsg(chatId int64) error {
	db := database.GetTgDB().Model(&model.TgClientMsg{})
	err := db.Delete(&model.TgClientMsg{}, "chat_id =? AND (type=? OR type=?)", chatId, model.Registration, model.Renewal).Error
	if err != nil {
		logger.Error(err)
		return err
	}
	return nil
}

func (t *TelegramService) DeleteMsg(id int64) error {
	db := database.GetTgDB()
	err := db.Model(&model.TgClientMsg{}).Delete(&model.TgClientMsg{}, id).Error
	if err != nil {
		logger.Error(err)
		return err
	}
	return nil
}
