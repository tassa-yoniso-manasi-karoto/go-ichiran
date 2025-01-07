
package ichiran

import (
	"strings"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)


type IchiranLogConsumer struct {
	Prefix      string
	ShowService bool
	ShowType    bool
	Level       zerolog.Level
	initChan    chan struct{}
	failedChan  chan error
}



func NewIchiranLogConsumer() *IchiranLogConsumer {
	return &IchiranLogConsumer{
		Prefix:      "ichiran",
		ShowService: true,
		ShowType:    true,
		Level:       zerolog.DebugLevel,
		initChan:    make(chan struct{}),
		failedChan:  make(chan error),
	}
}

func (l *IchiranLogConsumer) Log(containerName, message string) {
	if strings.Contains(message, "All set, awaiting commands") {
		select {
		case l.initChan <- struct{}{}:
		default: // Channel already closed or message already sent
		}
	}

	// Regular logging
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			event := log.Debug()
			if l.Level != zerolog.DebugLevel {
				event = log.WithLevel(l.Level)
			}

			if l.ShowService {
				event = event.Str("service", containerName)
			}
			if l.ShowType {
				event = event.Str("type", "stdout")
			}
			if l.Prefix != "" {
				event = event.Str("component", l.Prefix)
			}

			event.Msg(line)
		}
	}
}

func (l *IchiranLogConsumer) Err(containerName, message string) {
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			event := log.Error()
			if l.ShowService {
				event = event.Str("service", containerName)
			}
			if l.ShowType {
				event = event.Str("type", "stderr")
			}
			if l.Prefix != "" {
				event = event.Str("component", l.Prefix)
			}

			event.Msg(line)
		}
	}
}

func (l *IchiranLogConsumer) Status(container, msg string) {
	event := log.Info()
	if l.ShowService {
		event = event.Str("service", container)
	}
	if l.ShowType {
		event = event.Str("type", "status")
	}
	if l.Prefix != "" {
		event = event.Str("component", l.Prefix)
	}

	event.Msg(msg)
}

func (l *IchiranLogConsumer) Register(container string) {
	log.Info().
		Str("container", container).
		Str("type", "register").
		Msg("container registered")
}



func placeholder3454446543() {
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}

