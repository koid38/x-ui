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
		key         string
		desc        string
		crmFunction bool
	}
)

const (
	StartCmdKey          = string("start")
	UsageCmdKey          = string("usage")
	RegisterCmdKey       = string("register")
	ReferToFriendsCmdKey = string("refer")
	ContactSupportCmdKey = string("support")
)

func CreateChatMenu(crmEnabled bool) []tgbotapi.BotCommand {
	commands := []commandEntity{
		{
			key:         UsageCmdKey,
			desc:        Tr("menuGetUsage"),
			crmFunction: false,
		},
		{
			key:         RegisterCmdKey,
			desc:        Tr("menuOrder"),
			crmFunction: true,
		},

		{
			key:         ReferToFriendsCmdKey,
			desc:        Tr("menuRefer"),
			crmFunction: true,
		},
		{
			key:         ContactSupportCmdKey,
			desc:        Tr("menuSupport"),
			crmFunction: false,
		},
	}

	menuItemCount := len(commands)
	if !crmEnabled {
		menuItemCount -= 2
	}

	tgCommands := make([]tgbotapi.BotCommand, 0, menuItemCount)
	for _, cmd := range commands {
		if cmd.crmFunction && !crmEnabled {
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

	crmEnabled, err := s.telegramService.settingService.GetTgCrmEnabled()
	if err != nil {
		crmEnabled = false
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
					s.telegramService.GetAllClientUsages(msg.Chat.ID)
					return nil
				} else {
					resp.Text = Tr("msgAlreadyRegistered")
				}
			}
		} else {
			resp = *s.telegramService.GetClientUsage(msg.Chat.ID, msg.CommandArguments())

			if client == nil {
				name := msg.Chat.FirstName + " " + msg.Chat.LastName + " @" + msg.Chat.UserName
				s.client = &model.TgClient{
					Enabled: true,
					ChatID:  msg.Chat.ID,
					Name:    name,
					Uid:     msg.CommandArguments(),
				}
				err = s.telegramService.AddTgClient(s.client)
			} else {
				if s.telegramService.CheckIfClientExists(msg.CommandArguments()) {
					client.Uid = msg.CommandArguments()
					err = s.telegramService.UpdateClient(client)
				}
			}
			if err != nil {
				resp.Text = Tr("msgInternalError")
				resp.ReplyMarkup = nil
			}
		}

	case RegisterCmdKey:
		if !crmEnabled {
			resp.Text = Tr("msgIncorrectCmd")
			break
		}

		client, _ := s.telegramService.getTgClient(msg.Chat.ID)
		s.client = client

		s.showAccListKeyboard(&resp)

		s.clientRequest = &model.TgClientMsg{
			ChatID: msg.Chat.ID,
			Type:   model.Registration,
		}

		s.state = RegAccTypeState

	case ReferToFriendsCmdKey:
		if !crmEnabled {
			resp.Text = Tr("msgIncorrectCmd")
			break
		}

		client, _ := s.telegramService.getTgClient(msg.Chat.ID)
		s.client = client

		if client == nil {
			resp.Text = Tr("msgNotRegistered")
			break
		}
		referToFriendsMsg, err := s.telegramService.settingService.GetTgReferToFriendsMsg()
		if err != nil {
			resp.Text = Tr("msgInternalError")
		}
		referToFriendsMsg = s.telegramService.replaceMarkup(&referToFriendsMsg, client.ChatID, "")
		resp.Text = referToFriendsMsg
		resp.ParseMode = tgbotapi.ModeHTML

	case ContactSupportCmdKey:
		contactSupportMsg, err := s.telegramService.settingService.GetTgContactSupportMsg()
		if err != nil {
			resp.Text = Tr("msgInternalError")
		}
		resp.Text = contactSupportMsg
		resp.ParseMode = tgbotapi.ModeHTML

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

	s.clientRequest.Msg += "Type: " + orderType

	if s.client == nil {
		name := msg.Chat.FirstName + " " + msg.Chat.LastName + " @" + msg.Chat.UserName
		s.client = &model.TgClient{
			Enabled: false,
			ChatID:  msg.Chat.ID,
			Name:    name,
		}
		s.state = RegEmailState
		resp.Text = Tr("msgEnterEmail")
	} else {
		moneyTransferInstructions, err := s.telegramService.settingService.GetTgMoneyTransferMsg()
		if err != nil {
			logger.Error("RegAccTypeState failed to get money transfer instructions: ", err)
			resp.Text = Tr("msgInternalError")
			s.state = IdleState
			return &resp
		}
		s.canAcceptPhoto = true // allow the client to send receipts
		resp.Text = moneyTransferInstructions
		s.state = SendReceiptState
	}

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
		moneyTransferInstructions, err := s.telegramService.settingService.GetTgMoneyTransferMsg()
		if err != nil {
			logger.Error("RegNoteState failed to get money transfer instructions: ", err)
			resp.Text = Tr("msgInternalError")
			s.state = IdleState
			return &resp
		}
		s.canAcceptPhoto = true // allow the client to send receipts
		resp.Text = moneyTransferInstructions
		s.state = SendReceiptState
	}

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
	if s.clientRequest == nil {
		resp.Text = Tr("msgInternalError")
		s.canAcceptPhoto = false
		s.state = IdleState
		return &resp
	}

	if msg.Photo == nil {
		resp.Text = Tr("msgIncorrectReceipt")
		return &resp
	}

	// Put the order up on the panel
	err := s.telegramService.PushTgClientMsg(s.clientRequest)
	if err != nil {
		logger.Error(err)
		resp.Text = Tr("msgInternalError")
	}

	err = s.telegramService.SendMsgToAdmin("New client request! Please visit the panel.")
	if err != nil {
		logger.Error("SendReceiptState failed to send msg to admin:", err)
	}

	s.canAcceptPhoto = false
	s.state = IdleState
	resp.Text = Tr("msgOrderRegistered")

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

func (s *TgSession) RenewAccount(chatId int64, uuid string) *tgbotapi.MessageConfig {
	crmEnabled, err := s.telegramService.settingService.GetTgCrmEnabled()
	if err != nil {
		crmEnabled = false
	}

	resp := tgbotapi.NewMessage(chatId, "")
	if !crmEnabled {
		resp.Text = Tr("msgNotActive")
		return &resp
	}

	client, _ := s.telegramService.getTgClient(chatId)
	s.client = client

	if client != nil {
		s.showAccListKeyboard(&resp)
		s.clientRequest = &model.TgClientMsg{
			ChatID: s.client.ChatID,
			Type:   model.Renewal,
			Msg:    "Acc: " + uuid + ",",
		}

		s.state = RegAccTypeState
	} else {
		resp.Text = Tr("msgNotRegisteredEnterLink")
		s.state = IdleState
	}
	return &resp
}
