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

func (j *TelegramService) GetAllClientUsages(chatId int64) {
	tgBottoken, err := j.settingService.GetTgBotToken()
	if err != nil || tgBottoken == "" {
		logger.Error("GetAllClientUsages failed, GetTgBotToken fail:", err)
		return
	}
	bot, err := tgbotapi.NewBotAPI(tgBottoken)
	if err != nil {
		logger.Error("Get tgbot error:", err)
		return
	}

	client, err := j.getTgClient(chatId)
	if err != nil {
		logger.Error(err)
		return
	}

	uuids := strings.Split(client.Uid, ",")

	crmEnabled := j.settingService.GetTgCrmEnabled()
	for _, uuid := range uuids {
		resp := j.GetClientUsage(chatId, uuid, crmEnabled)
		bot.Send(resp)
	}
}

func (j *TelegramService) GetClientUsage(chatId int64, uuid string, showRenewBtn bool) *tgbotapi.MessageConfig {

	resp := tgbotapi.NewMessage(chatId, "")

	traffic, err := j.inboundService.GetClientTrafficById(uuid)
	if err != nil {
		logger.Error(err)
		resp.Text = Tr("incorrectUuid")
		return &resp
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
	resp.Text += fmt.Sprintf("💡 Active: %t\r\n📧 Name: %s\r\n🔄 Total: %s / %s\r\n📅 Expires on: %s\r\n\r\n",
		traffic.Enable, traffic.Email, common.FormatTraffic((traffic.Up + traffic.Down)),
		total, expiryTime)

	buttons := tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(Tr("update"), "update:"+uuid))
	if showRenewBtn {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(Tr("renew"), "renew:"+uuid))
	}
	resp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons)
	return &resp
}

func (j *TelegramService) CheckIfClientExists(uuid string) bool {
	if strings.TrimSpace(uuid) == "" {
		return false
	}
	_, err := j.inboundService.GetClientTrafficById(uuid)
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
	dbClient, err := t.getTgClient(client.ChatID)
	if err == nil && dbClient.Uid != "" {
		if !strings.Contains(dbClient.Uid, client.Uid) {
			client.Uid = dbClient.Uid + "," + client.Uid
		} else {
			client.Uid = dbClient.Uid
		}
	}
	return db.Save(client).Error
}

func (t *TelegramService) RegisterClient(client *model.TgClient) error {
	uuid := client.Uid
	err := t.UpdateClient(client)
	if err != nil {
		logger.Error(err)
		return err
	}

	finalMsg, err := t.settingService.GetTgCrmRegFinalMsg()
	if err != nil {
		logger.Error(err)
		finalMsg = Tr("msgAccCreateSuccess")
	}
	finalMsg = t.replaceMarkup(&finalMsg, client.ChatID, uuid)
	t.SendMsgToTgBot(client.ChatID, finalMsg)
	return nil
}

func (t *TelegramService) RenewClient(client *model.TgClient) error {
	err := t.UpdateClient(client)
	if err != nil {
		logger.Error(err)
		return err
	}

	finalMsg := Tr("msgRenewSuccess")
	t.SendMsgToTgBot(client.ChatID, finalMsg)
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

func (t *TelegramService) replaceMarkup(msg *string, chatId int64, uuid string) string {
	replacer := strings.NewReplacer("<UUID>", uuid, "<CHAT_ID>", strconv.FormatInt(chatId, 10))
	return replacer.Replace(*msg)
}

func (t *TelegramService) HandleMessage(msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	if _, exists := TgSessions[msg.Chat.ID]; !exists {
		TgSessions[msg.Chat.ID] = InitFSM()
	}
	return TgSessions[msg.Chat.ID].state(TgSessions[msg.Chat.ID], msg)
}

func (t *TelegramService) HandleCallback(callback *tgbotapi.CallbackQuery) (resp *tgbotapi.MessageConfig, delete bool, update bool) {

	chatId := callback.Message.Chat.ID
	if strings.HasPrefix(callback.Data, "update:") {
		resp = t.GetClientUsage(chatId, strings.TrimPrefix(callback.Data, "update:"), t.settingService.GetTgCrmEnabled())
		delete = false
		update = true
		return
	} else if strings.HasPrefix(callback.Data, "renew:") {
		if _, exists := TgSessions[callback.Message.Chat.ID]; !exists {
			TgSessions[chatId] = InitFSM()
		}
		resp = TgSessions[chatId].RenewAccount(chatId, strings.TrimPrefix(callback.Data, "renew:"))
		delete = false
		update = false
		return
	}

	resp = t.HandleMessage(&tgbotapi.Message{
		Chat: &tgbotapi.Chat{
			ID:        callback.Message.Chat.ID,
			UserName:  callback.From.UserName,
			FirstName: callback.From.FirstName,
			LastName:  callback.From.LastName,
		},
		Text: callback.Data,
	})
	delete = true
	update = false

	return
}

func (t *TelegramService) CanAcceptPhoto(chatId int64) bool {
	if _, exists := TgSessions[chatId]; !exists {
		TgSessions[chatId] = InitFSM()
	}
	return TgSessions[chatId].canAcceptPhoto
}

func (t *TelegramService) SendMsgToTgBot(chatId int64, msg string) error {

	tgBottoken, err := t.settingService.GetTgBotToken()
	if err != nil || tgBottoken == "" {
		logger.Error("SendMsgToTgBot failed, GetTgBotToken fail:", err)
		return err
	}
	bot, err := tgbotapi.NewBotAPI(tgBottoken)
	if err != nil {
		logger.Error("SendMsgToTgBot failed, NewBotAPI fail:", err)
		return err
	}

	info := tgbotapi.NewMessage(chatId, msg)
	info.ParseMode = "HTML"
	info.DisableWebPagePreview = true
	bot.Send(info)
	return nil
}

func (t *TelegramService) SendMsgToAdmin(msg string) error {
	adminId, err := t.settingService.GetTgBotChatId()
	if err != nil {
		logger.Error("SendMsgToAdmin failed, NewBotAPI fail:", err)
		return err
	}
	t.SendMsgToTgBot(int64(adminId), msg)
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
