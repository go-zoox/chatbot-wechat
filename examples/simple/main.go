package simple

import (
	"github.com/go-zoox/chatbot-wechat"
	"github.com/go-zoox/logger"
)

func main() {
	bot, err := chatbot.New(&chatbot.Config{
		AdminNickname: "Zero",
		ReprtURL:      "http://xxxx.com",
	})
	if err != nil {
		logger.Errorf("failed to create bot: %v", err)
		return
	}

	bot.OnCommand("/chatgpt", &chatbot.Command{
		ArgsLength: 2,
		Handler: func(args []string, request *chatbot.EventRequest, reply func(content string, msgType ...string) error) error {
			return nil
		},
	})

	if err := bot.Run(); err != nil {
		logger.Fatalf("%v", err)
	}
}
