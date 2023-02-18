package service

import (
	"fmt"
	"net/mail"
	"strings"
	"x-ui/database/model"
	"x-ui/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var TgSessions map[int64]*TgSession = make(map[int64]*TgSession)

type TgSession struct {
	State           stateFn
	telegramService TelegramService
	client          *model.TgClient
	clientRequest   *model.TgClientMsg
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

func CreateChatMenu(crmEnabled bool) []tgbotapi.BotCommand {
	commands := []commandEntity{
		{
			key:  StartCmdKey,
			desc: "Start",
		},
		{
			key:  UsageCmdKey,
			desc: "Get usage",
		},
		{
			key:  RegisterCmdKey,
			desc: "Order a new account",
		},
		{
			key:  StatusCmdKey,
			desc: "Bot status",
		},
	}

	menuItemCount := len(commands)
	if !crmEnabled {
		menuItemCount--
	}

	tgCommands := make([]tgbotapi.BotCommand, 0, menuItemCount)
	for _, cmd := range commands {
		if cmd.key == RegisterCmdKey && !crmEnabled {
			continue
		}
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

	case StatusCmdKey:
		resp.Text = "Bot is OK!"

	case RegisterCmdKey:
		crmEnabled, err := s.telegramService.settingService.GetTgCrmEnabled()
		if err != nil || !crmEnabled {
			resp.Text = "I don't know that command, choose an item from the menu"
			break
		}

		client, _ := s.telegramService.getTgClient(msg.Chat.ID)
		s.client = client

		if client == nil {
			accList, err := s.telegramService.settingService.GetTgCrmRegAccList()
			if err != nil {
				resp.Text = "Internal error"
				break
			}

			accList = strings.TrimSpace(accList)
			var buttons []tgbotapi.KeyboardButton
			accounts := strings.Split(accList, "\n")
			for i := 1; i <= len(accounts); i++ {
				buttons = append(buttons, tgbotapi.NewKeyboardButton(fmt.Sprint(i)))
			}
			if len(buttons) > 0 {
				replyKeyboard := tgbotapi.NewOneTimeReplyKeyboard(
					buttons,
				)
				resp.ReplyMarkup = replyKeyboard
			}
			s.State = RegAccTypeState
			resp.Text = "Please choose the type of account you would like to order.\n" + accList
		} else {
			resp.Text = "You have already registered. We will contact you soon."
		}

	case UsageCmdKey:
		if msg.CommandArguments() == "" {
			client, err := s.telegramService.getTgClient(msg.Chat.ID)
			if err != nil {
				resp.Text = "You're not registered in the system. If you already have an account with us, please enter your UID:"
				s.State = RegUuidState
			} else {
				if client.Enabled {
					resp.Text = s.telegramService.GetClientUsage(client.Uid)
				} else {
					resp.Text = "You have already registered. We will contact you soon."
				}
			}

		} else {
			resp.Text = s.telegramService.GetClientUsage(msg.CommandArguments())
		}
	default:
		resp.Text = "I don't know that command, choose an item from the menu"

	}
	return &resp

}

func RegAccTypeState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {

	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	orderType := strings.TrimSpace(msg.Text)
	if orderType == "" {
		resp.Text = "Please choose a number from the list."
		s.State = IdleState
		return &resp
	}

	s.client = &model.TgClient{
		Enabled: false,
		ChatID:  msg.Chat.ID,
	}

	s.clientRequest = &model.TgClientMsg{
		ChatID: s.client.ChatID,
		Type:   model.Registration,
		Msg:    orderType,
	}

	s.State = RegNameState
	resp.Text = "Enter your full name:"
	return &resp
}

func RegNameState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {

	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	name := strings.TrimSpace(msg.Text)
	if name == "" {
		resp.Text = "Incorrect format. Please enter your full name:"
		// s.Response.ParseMode = "HTML"
		s.State = IdleState
		return &resp
	}

	s.client.Name = name

	s.State = RegEmailState
	resp.Text = "Please enter a valid email address:"
	return &resp
}

func RegEmailState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {

	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	email := strings.TrimSpace(msg.Text)
	if _, err := mail.ParseAddress(email); err != nil {
		resp.Text = "Incorrect email. Please enter a valid email address:"
		resp.ParseMode = "HTML"
		// s.State = IdleState
		return &resp
	}

	s.client.Email = email
	err := s.telegramService.AddTgClient(s.client)

	if err != nil {
		logger.Error(err)
		resp.Text = "Error during registration"
	} else {
		if s.client.Enabled && s.client.Uid != "" {
			resp.Text = "Congratulations! You are now registered in the system."
		} else {
			err := s.telegramService.PushTgClientMsg(s.clientRequest)
			if err != nil {
				logger.Error(err)
				resp.Text = "Error during registration"
			} else {

				finalMsg, err := s.telegramService.settingService.GetTgCrmRegFinalMsg()
				if err != nil {
					logger.Error(err)
					finalMsg = "Thank you for your order. You will be contacted via email soon."
				}

				resp.Text = finalMsg
			}
		}
	}
	s.State = IdleState
	return &resp
}

func RegUuidState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {

	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	uuid := strings.TrimSpace(msg.Text)
	if !s.telegramService.CheckIfClientExists(uuid) {
		resp.Text = "UUID doesn't exist in the database. E.g.\nfc3239ed-8f3b-4151-ff51-b183d5182142\nPlease enter a correct UUID:"
		resp.ParseMode = "HTML"
		return &resp
	}
	s.client = &model.TgClient{
		ChatID:  msg.Chat.ID,
		Uid:     uuid,
		Enabled: true,
	}

	s.State = RegNameState
	resp.Text = "Enter your full name:"
	return &resp
}

func abort(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	s.State = IdleState
	s.client = nil
	return IdleState(s, msg)
}
