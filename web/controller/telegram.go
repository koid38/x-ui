package controller

import (
	"strconv"
	"x-ui/logger"
	"x-ui/web/service"

	"github.com/gin-gonic/gin"
)

type TelegramController struct {
	telegramService service.TelegramService
	xrayService     service.XrayService
}

func NewTelegramController(g *gin.RouterGroup) *TelegramController {
	a := &TelegramController{}
	a.initRouter(g)
	// a.startTask()
	return a
}

func (a *TelegramController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/tgClients")

	g.POST("/list", a.getClients)
	g.POST("/del/:id", a.delClient)

}

func (a *TelegramController) getClients(c *gin.Context) {
	logger.Warning("Getting tg clients")
	// user := session.GetLoginUser(c)
	clients, err := a.telegramService.GetTgClients()
	if err != nil {
		jsonMsg(c, I18n(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	jsonObj(c, clients, nil)
}

func (a *TelegramController) delClient(c *gin.Context) {
	logger.Warning("Deleting client")
	// user := session.GetLoginUser(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		jsonMsg(c, I18n(c, "get"), err)
		return
	}
	err = a.telegramService.DeleteClient(id)
	jsonMsgObj(c, I18n(c, "delete"), id, err)
	if err != nil {
		jsonMsg(c, I18n(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
}
