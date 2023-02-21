package chatbot

import (
	gstrings "strings"

	"github.com/eatmoreapple/openwechat"
	"github.com/go-zoox/chatbot-wechat/command"
	"github.com/go-zoox/core-utils/fmt"
	"github.com/go-zoox/core-utils/strings"
	"github.com/go-zoox/debug"
	feishuWebhook "github.com/go-zoox/feishu/webhook"
	"github.com/go-zoox/logger"
)

type EventRequest = openwechat.Message

type Admin = openwechat.Friend

type MessageReply = func(contentx string, msgType ...string) error

type OnMessageHandler = func(content string, request *EventRequest, reply MessageReply) error

type OnOfflineHandler = func(request *EventRequest, reply MessageReply) error

type Info = openwechat.Self

type Command struct {
	ArgsLength int `json:"args_length,omitempty"`
	IsAllow    func(request *EventRequest) (ok bool, err error)
	Handler    func(args []string, request *EventRequest, reply MessageReply) error
}

// ChatBot is the chatbot interface.
type ChatBot interface {
	OnMessage(handler OnMessageHandler) error
	OnCommand(command string, handler *Command) error
	Run() error
	//
	SetOnline() error
	SetOffline() error
	//
	OnOffline(handler OnOfflineHandler) error
	//
	Info() (info *Info, err error)
}

// Config is the configuration for create chatbot.
type Config struct {
	AdminNickname string
	ReprtURL      string
}

type chatbot struct {
	cfg       *Config
	onMessage OnMessageHandler
	onOffline OnOfflineHandler
	commands  map[string]*Command
	//
	isOffline bool
	//
	self *openwechat.Self
	//
	bot *openwechat.Bot
}

// New creates a new chatbot
func New(cfg *Config) (ChatBot, error) {
	return &chatbot{
		cfg: cfg,
		commands: map[string]*Command{
			"ping": {
				Handler: func(args []string, request *EventRequest, reply MessageReply) error {
					reply("pong")
					return nil
				},
			},
		},
	}, nil
}

func (c *chatbot) Info() (info *Info, err error) {
	if c.self == nil {
		c.self, err = c.bot.GetCurrentUser()
		if err != nil {
			return nil, fmt.Errorf("failed to get robot user(1): %v", err)
		}
	}

	return c.self, nil
}

func (c *chatbot) OnMessage(handler OnMessageHandler) error {
	if c.onMessage != nil {
		return fmt.Errorf("on message is already registered")
	}

	c.onMessage = handler
	return nil
}

func (c *chatbot) OnCommand(command string, handler *Command) error {
	if _, ok := c.commands[command]; ok {
		return fmt.Errorf("failed to register command %s, which is already registered before", command)
	}

	logger.Infof("register command: %s", command)
	c.commands[command] = handler
	return nil
}

func (c *chatbot) OnOffline(handler OnOfflineHandler) error {
	if c.onOffline != nil {
		return fmt.Errorf("on message is already registered")
	}

	c.onOffline = handler
	return nil
}

// Run starts a application server.
func (c *chatbot) Run() (err error) {
	c.bot = openwechat.DefaultBot(openwechat.Desktop)
	bot := c.bot

	var admin *Admin

	bot.MessageHandler = func(msg *openwechat.Message) {
		// exit if not a text message
		if !msg.IsText() {
			return
		}

		if debug.IsDebugMode() {
			fmt.PrintJSON(msg)
		}

		isAdmin := func() bool {
			debug.Debug("checking admin: %s == %s ? %v", msg.FromUserName, admin.UserName, msg.FromUserName == admin.UserName)
			// @TODO 群聊识别用户
			if msg.IsSendByGroup() {
				return admin != nil && msg.FromUserName == admin.UserName
			}

			return admin != nil && msg.FromUserName == admin.UserName
		}

		handleReply := func(content string, msgType ...string) error {
			_, err := msg.ReplyText(content)
			if err != nil {
				return fmt.Errorf("failed to reply to command: %v", err)
			}

			return nil
		}

		// Check is offline
		if c.isOffline {
			if c.onOffline != nil {
				if err := c.onOffline(msg, handleReply); err != nil {
					logger.Errorf("failed to handdle offline: %v", err)
				}
				return
			}

			if err := handleReply("bot is offline"); err != nil {
				logger.Errorf("failed to reply when offline: %v", err)
			}
			return
		}

		// @TODO 群聊无法区分 Admin 用户 ID，导致无法在群里使用命令
		if !msg.IsSendByGroup() {
			// Checking Commands
			isCommand := false
			commandText := ""
			if isAdmin() {
				logger.Infof("is admin")
				rawCommand := msg.Content
				if strings.StartsWith(msg.Content, fmt.Sprintf("@%s", c.self.NickName)) {
					rawCommand = rawCommand[len(fmt.Sprintf("@%s", c.self.NickName))+1:]
					rawCommand = gstrings.TrimLeft(rawCommand, "\ufffd")
				}

				if rawCommand[0] != '/' {
					rawCommand = fmt.Sprintf("/%s", rawCommand)
				}

				isCommand = true
				commandText = rawCommand
			} else {
				logger.Infof("not admin")
				// remove @XXX COMMAND
				rawCommand := msg.Content
				if strings.StartsWith(msg.Content, fmt.Sprintf("@%s", c.self.NickName)) {
					rawCommand = rawCommand[len(fmt.Sprintf("@%s", c.self.NickName))+1:]
					rawCommand = gstrings.TrimLeft(rawCommand, "\ufffd")
					logger.Infof("raw command: %s", rawCommand)

					if command.IsCommand(rawCommand) {
						isCommand = true
						commandText = rawCommand
					}
				}
			}

			logger.Infof("is command(%s): %v", commandText, isCommand)

			if isCommand {
				cmd, arg, err := command.ParseCommandWithArg(commandText)
				if err != nil {
					logger.Errorf("failed to parse command(%s): %v", commandText, err)
					return
				}

				logger.Infof("onCommand start: %s", commandText)
				if err := c.handleCommand(admin, cmd, arg, msg, handleReply); err != nil {
					logger.Errorf("failed to parse command(%s): %v", commandText, err)
				}
				return
			}
		}

		// @TODO specifical command: *, used for common message
		if cmd, ok := c.commands["*"]; ok {
			if err := cmd.Handler([]string{msg.Content}, msg, handleReply); err != nil {
				logger.Errorf("failed to handle command * with common message): %v", err)
				return
			}
		}

		logger.Infof("onMessage start")

		// Common Message
		err := c.onMessage(msg.Content, msg, func(content string, msgType ...string) error {
			_, err := msg.ReplyText(content)
			if err != nil {
				return fmt.Errorf("failed to reply to command: %v", err)
			}

			return nil
		})
		if err != nil {
			logger.Errorf("failed to on message: %v", err)
		}
	}

	// 注册登陆二维码回调
	bot.UUIDCallback = func(uuid string) {
		qrcodeUrl := openwechat.GetQrcodeUrl(uuid)
		if c.cfg.ReprtURL != "" {
			logger.Infof("二维码访问地址已发送到飞书: %s", c.cfg.ReprtURL)
			token := strings.Replace(c.cfg.ReprtURL, feishuWebhook.BaseURI+"/", "", 1)
			if token[len(token)-1] == '/' {
				token = token[:len(token)-1]
			}

			if token == "" {
				logger.Errorf("无效的飞书机器人 Webhook: %s", c.cfg.ReprtURL)
				return
			}

			f := feishuWebhook.New(token)
			if err := f.SendText(fmt.Sprintf("微信机器人登录二维码：%s", qrcodeUrl)); err != nil {
				logger.Errorf("failed to report url %s", c.cfg.ReprtURL)
			}
		} else {
			logger.Infof("访问下面网址扫描二维码登录")
			logger.Infof(qrcodeUrl)
		}
	}

	// 登陆
	if err := bot.Login(); err != nil {
		return fmt.Errorf("failed to login: %v", err)
	}

	// 获取登陆的用户
	if c.self == nil {
		c.self, err = bot.GetCurrentUser()
		if err != nil {
			return fmt.Errorf("failed to get robot user(2): %v", err)
		}
	}

	if c.cfg.AdminNickname != "" {
		friends, err := c.self.Friends()
		if err != nil {
			return fmt.Errorf("failed to list friends: %v", err)
		}
		admin = friends.GetByNickName(c.cfg.AdminNickname)
	}

	fmt.PrintJSON(map[string]any{
		"cfg":   c.cfg,
		"bot":   c.self,
		"admin": admin,
	})

	return bot.Block()
}

func (c *chatbot) handleCommand(admin *Admin, cmd, arg string, msg *EventRequest, reply MessageReply) error {
	isAdmin := func() bool {
		return admin != nil && msg.FromUserName == admin.UserName
	}

	logger.Infof("search command: %s - %d", cmd, len(cmd))
	if c, ok := c.commands[cmd]; ok {
		logger.Infof("command found: %s", cmd)
		isAllowRunCommand := false
		if isAdmin() {
			isAllowRunCommand = true
		} else if c.IsAllow != nil {
			ok, err := c.IsAllow(msg)
			if err != nil {
				return fmt.Errorf("failed to check permission with isAllow(command: %s): %v", cmd, err)
			}

			if ok {
				isAllowRunCommand = true
			}
		}

		logger.Infof("isAllowRunCommand: %v", isAllowRunCommand)
		if !isAllowRunCommand {
			return fmt.Errorf("user(%s) not allowed to run command(%s)", cmd)
		}

		logger.Infof("handle command with args: %s (length: %d)", arg, len(strings.SplitN(arg, " ", c.ArgsLength)))
		err := c.Handler(strings.SplitN(arg, " ", c.ArgsLength), msg, reply)
		if err != nil {
			return fmt.Errorf("failed to run command(%s): %v", cmd, err)
		}
	} else {
		logger.Infof("command not found: %s", cmd)
	}

	return nil
}

func (c *chatbot) SetOnline() error {
	c.isOffline = false
	return nil
}

func (c *chatbot) SetOffline() error {
	c.isOffline = false
	return nil
}
