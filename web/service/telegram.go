package service

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/logger"
	"x-ui/util/common"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"
)

type TelegramService struct {
	inboundService InboundService
}

var TgSessions map[int64]*TgSession = make(map[int64]*TgSession)

type TgSession struct {
	State           stateFn
	telegramService TelegramService
	client          *model.TgClient
}

type stateFn func(*TgSession, *tgbotapi.Message) *tgbotapi.MessageConfig

type (
	commandEntity struct {
		key  string
		desc string
		//		action func(upd tgbotapi.Update)
	}
)

const (
	StartCmdKey    = string("start")
	UsageCmdKey    = string("usage")
	RegisterCmdKey = string("register")
	StatusCmdKey   = string("status")
)

func CreateChatMenu() []tgbotapi.BotCommand {
	commands := []commandEntity{
		{
			key:  StartCmdKey,
			desc: "Start",
			// action: bot.StartCmd,
		},
		{
			key:  UsageCmdKey,
			desc: "Get usage",
			// action: j.getClientUsage(update.Message.CommandArguments()),
		},
		{
			key:  RegisterCmdKey,
			desc: "Register for an account",
			// action: b.ReviewsCmd,
		},
		{
			key:  StatusCmdKey,
			desc: "Bot status",
			// action: b.ReviewsCmd,
		},
	}
	tgCommands := make([]tgbotapi.BotCommand, 0, len(commands))
	for _, cmd := range commands {
		//		bot.commands[cmd.key] = cmd
		tgCommands = append(tgCommands, tgbotapi.BotCommand{
			Command:     "/" + string(cmd.key),
			Description: cmd.desc,
		})
	}
	return tgCommands
}

//***************************************************************************
// States
//***************************************************************************

func InitFSM() *TgSession {
	return &TgSession{State: IdleState}
}

func IdleState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	resp := tgbotapi.NewMessage(msg.Chat.ID, "")

	if !msg.IsCommand() {
		resp.Text = "Choose an item from the menu"
		return &resp
	}

	// Extract the command from the Message.
	switch msg.Command() {
	case StartCmdKey:
		resp.Text = "Hi!\nYou can use the menu to get your usage"
		//			msg.ReplyMarkup = numericKeyboard

	case "status":
		resp.Text = "Bot is OK!"

	case "register":
		client, err := s.telegramService.getTgClient(msg.Chat.ID)
		if err == nil {
			s.client = nil
		} else {
			s.client = client
		}

		if s.client == nil {
			s.State = RegNameState
			resp.Text = "Enter your full name:"
		} else {
			resp.Text = "You have already registered. We will contact you soon."
		}

	case "usage":
		if msg.CommandArguments() == "" {
			resp.Text = "To get your usage, send a message like this:\nExample : <code>/usage fc3239ed-8f3b-4151-ff51-b183d5182142</code>"
			resp.ParseMode = "HTML"
		} else {
			resp.Text = s.telegramService.GetClientUsage(msg.CommandArguments())
		}
	default:
		resp.Text = "I don't know that command, /start"
		//			msg.ReplyMarkup = numericKeyboard

	}
	return &resp

}

func RegNameState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	resp := tgbotapi.NewMessage(msg.Chat.ID, "")

	if msg.IsCommand() {
		abortRegistration(s, &resp)
		return &resp
	}

	name := strings.TrimSpace(msg.Text)
	if name == "" {
		resp.Text = "Incorrect format. Please enter your full name:"
		// s.Response.ParseMode = "HTML"
		s.State = IdleState
		return &resp
	}
	if s.client == nil {
		s.client = new(model.TgClient)
	}
	s.client.ChatID = msg.Chat.ID
	s.client.Name = name
	s.State = RegEmailState
	resp.Text = "Please enter a valid email address:"
	return &resp
}

func RegEmailState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	resp := tgbotapi.NewMessage(msg.Chat.ID, "")

	if msg.IsCommand() {
		abortRegistration(s, &resp)
		return &resp
	}

	email := strings.TrimSpace(msg.Text)
	if _, err := mail.ParseAddress(email); err != nil {
		resp.Text = "Incorrect email. Please enter a valid email address:"
		resp.ParseMode = "HTML"
		s.State = IdleState
		return &resp
	}
	s.client.Email = email
	s.State = IdleState
	resp.Text = "Thank you for signing up. You will be contacted via email soon."
	if err := s.telegramService.AddTgClient(s.client); err != nil {
		logger.Warning(err)
	} else {
		resp.Text = "Thank you for signing up. You will be contacted via email soon."
	}
	return &resp
}

func abortRegistration(s *TgSession, resp *tgbotapi.MessageConfig) {
	resp.Text = "Registration aborted"
	s.State = IdleState
	s.client = nil
}

func (j *TelegramService) GetClientUsage(id string) string {
	traffic, err := j.inboundService.GetClientTrafficById(id)
	if err != nil {
		logger.Warning(err)
		return "something wrong!"
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

	return output
}

func (t *TelegramService) AddTgClient(client *model.TgClient) error {
	db := database.GetTgDB()
	err := db.Create(client).Error
	return err
}

func (t *TelegramService) GetTgClients() ([]*model.TgClient, error) {
	db := database.GetTgDB()
	var clients []*model.TgClient
	err := db.Model(model.TgClient{}).Find(&clients).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		logger.Warning(err)
		return nil, err
	}
	logger.Warning(clients)
	return clients, nil
}

func (t *TelegramService) ApproveClient(id int64) ([]*model.TgClient, error) {
	db := database.GetTgDB()
	var clients []*model.TgClient
	err := db.Model(model.TgClient{}).Find(&clients).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		logger.Warning(err)
		return nil, err
	}
	logger.Warning(clients)
	return clients, nil
}

func (t *TelegramService) DeleteClient(id int64) error {
	db := database.GetTgDB()
	var clients []*model.TgClient
	err := db.Model(model.TgClient{}).Delete(model.TgClient{}, id).Error
	if err != nil {
		logger.Warning(err)
		return err
	}
	logger.Warning(clients)
	return nil
}

func (t *TelegramService) getTgClient(id int64) (*model.TgClient, error) {
	db := database.GetTgDB()
	var client *model.TgClient
	err := db.Model(model.TgClient{}).First(&client, id).Error
	if err != nil {
		logger.Warning(err)
		return nil, err
	}
	logger.Warning(client.ChatID)
	return client, nil
}

// func (t *TelegramService) checkTgClientExists(client *model.TgClient) (string, error) {
// 	clients, err := t.getTgClients()
// 	if err != nil {
// 		return "", err
// 	}
// 	emails := make(map[string]bool)
// 	for _, client := range clients {
// 		if client.Email != "" {
// 			if emails[client.Email] {
// 				return client.Email, nil
// 			}
// 			emails[client.Email] = true
// 		}
// 	}
// 	return s.checkEmailsExist(emails, inbound.Id)
// }

func (t *TelegramService) HandleMessage(msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	if _, exists := TgSessions[msg.Chat.ID]; !exists {
		TgSessions[msg.Chat.ID] = InitFSM()
	}
	return TgSessions[msg.Chat.ID].State(TgSessions[msg.Chat.ID], msg)
}
