package service

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"x-ui/database/model"
	"x-ui/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var TgSessions map[int64]*TgSession = make(map[int64]*TgSession)

type TgSession struct {
	state           stateFn
	canAcceptPhoto  bool
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
	StartCmdKey       = string("start")
	UsageCmdKey       = string("usage")
	RegisterCmdKey    = string("register")
	RenewCmdKey       = string("renew")
	SendReceiptCmdKey = string("receipt")
	ResetCmdKey       = string("reset")
)

func CreateChatMenu(crmEnabled bool) []tgbotapi.BotCommand {
	commands := []commandEntity{
		{
			key:  UsageCmdKey,
			desc: Tr("menuGetUsage"),
		},
		{
			key:  RegisterCmdKey,
			desc: Tr("menuOrder"),
		},
		{
			key:  RenewCmdKey,
			desc: Tr("menuRenew"),
		},
		{
			key:  SendReceiptCmdKey,
			desc: Tr("menuUploadReceipt"),
		},
		{
			key:  ResetCmdKey,
			desc: Tr("menuReset"),
		},
	}

	menuItemCount := len(commands)
	if !crmEnabled {
		menuItemCount -= 3
	}

	tgCommands := make([]tgbotapi.BotCommand, 0, menuItemCount)
	for _, cmd := range commands {
		if (cmd.key == RegisterCmdKey || cmd.key == RenewCmdKey || cmd.key == SendReceiptCmdKey) && !crmEnabled {
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
	return &TgSession{
		state:          IdleState,
		canAcceptPhoto: false,
	}
}

func IdleState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	resp := tgbotapi.NewMessage(msg.Chat.ID, "")

	if !msg.IsCommand() {
		resp.Text = Tr("msgChooseFromMenu")
		return &resp
	}

	// Extract the command from the Message.
	switch msg.Command() {
	case StartCmdKey:
		resp.Text = Tr("msgChooseFromMenu")

	case UsageCmdKey:
		client, err := s.telegramService.getTgClient(msg.Chat.ID)
		if msg.CommandArguments() == "" {
			if err != nil {
				resp.Text = Tr("msgNotRegisteredEnterLink")
				s.state = RegUuidState
			} else {
				if client.Enabled {
					resp.Text, _ = s.telegramService.GetClientUsage(client.Uid)
				} else {
					resp.Text = Tr("msgAlreadyRegistered")
				}
			}

		} else {
			resp.Text, err = s.telegramService.GetClientUsage(msg.CommandArguments())

			if client == nil && err == nil {
				name := msg.Chat.FirstName + " " + msg.Chat.LastName + " @" + msg.Chat.UserName
				s.client = &model.TgClient{
					Enabled: true,
					ChatID:  msg.Chat.ID,
					Name:    name,
					Uid:     msg.CommandArguments(),
				}
				err = s.telegramService.AddTgClient(s.client)
			}
		}

	case RegisterCmdKey:
		crmEnabled, err := s.telegramService.settingService.GetTgCrmEnabled()
		if err != nil || !crmEnabled {
			resp.Text = Tr("msgIncorrectCmd")
			break
		}

		client, _ := s.telegramService.getTgClient(msg.Chat.ID)
		s.client = client

		if client == nil {

			s.showAccListKeyboard(&resp)

			s.clientRequest = &model.TgClientMsg{
				ChatID: msg.Chat.ID,
				Type:   model.Registration,
			}

			s.state = RegAccTypeState
		} else {
			resp.Text = Tr("msgErrorMultipleAcc")
		}

	case RenewCmdKey:
		crmEnabled, err := s.telegramService.settingService.GetTgCrmEnabled()
		if err != nil || !crmEnabled {
			resp.Text = Tr("msgIncorrectCmd")
			break
		}

		client, _ := s.telegramService.getTgClient(msg.Chat.ID)
		s.client = client

		if client != nil {

			s.showAccListKeyboard(&resp)

			s.clientRequest = &model.TgClientMsg{
				ChatID: s.client.ChatID,
				Type:   model.Renewal,
			}

			s.state = RegAccTypeState
		} else {
			resp.Text = Tr("msgNotRegisteredEnterLink")
			s.state = RegUuidState
		}

	case SendReceiptCmdKey:
		crmEnabled, err := s.telegramService.settingService.GetTgCrmEnabled()
		if err != nil || !crmEnabled {
			resp.Text = Tr("msgIncorrectCmd")
			break
		}

		client, _ := s.telegramService.getTgClient(msg.Chat.ID)
		s.client = client

		if client != nil {
			s.canAcceptPhoto = true
			s.state = SendReceiptState
			resp.Text = Tr("msgAttachReceipt")
		} else {
			resp.Text = Tr("msgNotRegistered")
		}

	case ResetCmdKey:
		client, _ := s.telegramService.getTgClient(msg.Chat.ID)
		s.client = client
		if client != nil {
			s.state = ConfirmResetState
			resp.Text = Tr("msgConfirmReset")
			resp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(Tr("yes"), "Yes"),
				tgbotapi.NewInlineKeyboardButtonData(Tr("no"), "No"),
			),
			)
		} else {
			resp.Text = Tr("msgNotRegistered")
		}

	default:
		resp.Text = Tr("msgIncorrectCmd")

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
		resp.Text = Tr("msgIncorrectPackageNo")
		s.state = IdleState
		return &resp
	}

	name := msg.Chat.FirstName + " " + msg.Chat.LastName + " @" + msg.Chat.UserName
	s.client = &model.TgClient{
		Enabled: false,
		ChatID:  msg.Chat.ID,
		Name:    name,
	}

	s.clientRequest.Msg = "Type: " + orderType

	if s.clientRequest.Type == model.Renewal {
		err := s.telegramService.PushTgClientMsg(s.clientRequest)
		if err != nil {
			logger.Error(err)
			resp.Text = Tr("msgInternalError")
		} else {
			s.telegramService.SendMsgToAdmin("New account renewal request! Please visit the panel.")
			if err != nil {
				logger.Error("RegNoteState failed to send msg to admin:", err)
			}

			s.canAcceptPhoto = true // allow the client to send receipts
			resp.Text = Tr("msgOrderRegistered")
			s.state = IdleState
		}
		return &resp
	}

	s.state = RegEmailState
	resp.Text = Tr("msgEnterEmail")
	return &resp
}

func RegEmailState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {

	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	email := strings.TrimSpace(msg.Text)
	if _, err := mail.ParseAddress(email); err != nil {
		resp.Text = Tr("msgIncorrectEmail")
		return &resp
	}

	s.client.Email = email
	resp.Text = Tr("msgAddNotes")

	s.state = RegNoteState
	return &resp
}

func RegNoteState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {

	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	note := strings.TrimSpace(msg.Text)

	s.clientRequest.Msg += ", Note: " + note
	err := s.telegramService.AddTgClient(s.client)

	if err != nil {
		logger.Error(err)
		resp.Text = Tr("msgInternalError")
	} else {
		err := s.telegramService.PushTgClientMsg(s.clientRequest)
		if err != nil {
			logger.Error(err)
			resp.Text = Tr("msgInternalError")
		} else {
			resp.Text = Tr("msgOrderRegistered")
		}
	}

	s.telegramService.SendMsgToAdmin("New account registration request! Please visit the panel.")
	if err != nil {
		logger.Error("RegNoteState failed to send msg to admin:", err)
	}

	s.canAcceptPhoto = true // allow the client to send receipts

	s.state = IdleState
	return &resp
}

func RegUuidState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {

	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	re := regexp.MustCompile("[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
	uuid := re.FindString(msg.Text)

	if uuid == "" || !s.telegramService.CheckIfClientExists(uuid) {
		resp.Text = Tr("msgIncorrectUuid")
		return &resp
	}
	s.client = &model.TgClient{
		ChatID:  msg.Chat.ID,
		Uid:     uuid,
		Enabled: true,
	}

	name := msg.Chat.FirstName + " " + msg.Chat.LastName + " @" + msg.Chat.UserName
	s.client = &model.TgClient{
		Enabled: true,
		ChatID:  msg.Chat.ID,
		Name:    name,
		Uid:     uuid,
	}

	err := s.telegramService.AddTgClient(s.client)
	if err != nil {
		logger.Error(err)
		resp.Text = Tr("msgInternalError")
	} else {
		resp.Text = Tr("msgRegisterSuccess")
	}

	s.state = IdleState
	return &resp
}

func SendReceiptState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	if msg.IsCommand() {
		return abort(s, msg)
	}

	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	if msg.Photo != nil {
		s.canAcceptPhoto = false
		s.state = IdleState
		resp.Text = Tr("msgReceiptReceived")
	} else {
		resp.Text = Tr("msgIncorrectReceipt")
	}
	return &resp
}

func ConfirmResetState(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	resp := tgbotapi.NewMessage(msg.Chat.ID, "")
	if strings.ToLower(msg.Text) == "yes" {
		err := s.telegramService.DeleteClient(msg.Chat.ID)
		if err == nil {
			resp.Text = Tr("msgResetSuccess")
		} else {
			resp.Text = Tr("msgInternalError")
		}
	} else {
		resp.Text = Tr("cancelled")
	}

	s.state = IdleState
	return &resp
}

/*********************************************************
* Helper functions
*********************************************************/

func abort(s *TgSession, msg *tgbotapi.Message) *tgbotapi.MessageConfig {
	s.state = IdleState
	s.client = nil
	s.canAcceptPhoto = false
	return IdleState(s, msg)
}

func (s *TgSession) showAccListKeyboard(resp *tgbotapi.MessageConfig) {
	accList, err := s.telegramService.settingService.GetTgCrmRegAccList()
	if err != nil {
		resp.Text = Tr("msgInternalError")
		return
	}

	accList = strings.TrimSpace(accList)
	accounts := strings.Split(accList, "\n")
	row := tgbotapi.NewInlineKeyboardRow()
	for i := 1; i <= len(accounts); i++ {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprint(i), fmt.Sprint(i)))
	}
	if len(row) > 0 {
		resp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(row)
	}

	resp.Text = Tr("msgChoosePackage") + "\n" + accList
}
