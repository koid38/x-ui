package service

import (
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
		client, _ := s.telegramService.getTgClient(msg.Chat.ID)
		s.client = client

		if s.client == nil {
			s.State = RegNameState
			resp.Text = "Enter your full name:"
		} else {
			resp.Text = "You have already registered. We will contact you soon."
		}

	case "usage":
		if msg.CommandArguments() == "" {
			client, err := s.telegramService.getTgClient(msg.Chat.ID)
			if err != nil {
				resp.Text = "You're not registered in the system. If you already have an account with us, please enter your UID:"
				s.State = RegUuidState
			} else {
				if client.Approved {
					resp.Text = s.telegramService.GetClientUsage(client.Uid)
				} else {
					resp.Text = "You have already registered. We will contact you soon."
				}
			}

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

	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	email := strings.TrimSpace(msg.Text)
	if _, err := mail.ParseAddress(email); err != nil {
		resp.Text = "Incorrect email. Please enter a valid email address:"
		resp.ParseMode = "HTML"
		s.State = IdleState
		return &resp
	}
	s.client.Email = email
	if !(s.client.Approved && s.client.Uid != "") {
		s.client.Uid = "not assigned"
		s.client.Approved = false
	}
	err := s.telegramService.AddTgClient(s.client)
	if err != nil {
		logger.Error(err)
		resp.Text = "Error during registration"
	} else {
		if s.client.Approved && s.client.Uid != "" {
			resp.Text = "Congratulations! You are now registered in the system."
		} else {
			resp.Text = "Thank you for signing up. You will be contacted via email soon."
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
		ChatID:   msg.Chat.ID,
		Uid:      uuid,
		Approved: true,
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
