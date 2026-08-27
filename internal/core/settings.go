package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/config"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

const maxDisplayBytes = 4096 // Bounds sound and icon fields within the 4 MiB settings/job envelope.

type Settings struct {
	Provider          string            `json:"provider"`
	MinSeconds        int64             `json:"minSeconds"`
	LongTaskSeconds   int64             `json:"longTaskSeconds"`
	MediumRingSeconds int               `json:"mediumRingSeconds"`
	LongRingSeconds   int               `json:"longRingSeconds"`
	Continuous        bool              `json:"continuous"`
	Level             string            `json:"level"`
	Volume            int               `json:"volume"`
	Sound             string            `json:"sound"`
	NtfyPriority      int               `json:"ntfyPriority"`
	EnableAttention   bool              `json:"enableAttention"`
	Icons             map[string]string `json:"icons"`
}

func Defaults() (Settings, error) {
	object, err := strictjson.Object(config.DefaultsJSON())
	if err != nil {
		return Settings{}, err
	}
	return applySettings(object, Settings{})
}

// ParseSettings applies a strict partial JSON object to an independent base copy.
func ParseSettings(patch []byte, base Settings) (Settings, error) {
	if err := ValidateSettings(base); err != nil {
		return Settings{}, err
	}
	object, err := strictjson.Object(patch)
	if err != nil {
		return Settings{}, err
	}
	return applySettings(object, copySettings(base))
}

func applySettings(patch map[string]json.RawMessage, settings Settings) (Settings, error) {
	for key, value := range patch {
		var err error
		switch key {
		case "provider":
			settings.Provider, err = requiredString(value, key)
		case "minSeconds":
			settings.MinSeconds, err = requiredInteger(value, key)
		case "longTaskSeconds":
			settings.LongTaskSeconds, err = requiredInteger(value, key)
		case "mediumRingSeconds":
			settings.MediumRingSeconds, err = requiredInt(value, key)
		case "longRingSeconds":
			settings.LongRingSeconds, err = requiredInt(value, key)
		case "continuous":
			settings.Continuous, err = requiredBoolean(value, key)
		case "level":
			settings.Level, err = requiredString(value, key)
		case "volume":
			settings.Volume, err = requiredInt(value, key)
		case "sound":
			settings.Sound, err = requiredString(value, key)
		case "ntfyPriority":
			settings.NtfyPriority, err = requiredInt(value, key)
		case "enableAttention":
			settings.EnableAttention, err = requiredBoolean(value, key)
		case "icons":
			icons, err := parseIcons(value)
			if err != nil {
				return Settings{}, err
			}
			settings.Icons = icons
		default:
			return Settings{}, fmt.Errorf("unknown setting %q", key)
		}
		if err != nil {
			return Settings{}, err
		}
	}
	if err := ValidateSettings(settings); err != nil {
		return Settings{}, err
	}
	return copySettings(settings), nil
}

func requiredString(value []byte, field string) (string, error) {
	result, err := strictjson.String(value)
	if err != nil {
		return "", settingTypeError(field)
	}
	return result, nil
}

func requiredInteger(value []byte, field string) (int64, error) {
	result, err := strictjson.Integer(value)
	if err != nil {
		return 0, settingTypeError(field)
	}
	return result, nil
}

func requiredInt(value []byte, field string) (int, error) {
	result, err := requiredInteger(value, field)
	if err != nil || int64(int(result)) != result {
		return 0, settingTypeError(field)
	}
	return int(result), nil
}

func requiredBoolean(value []byte, field string) (bool, error) {
	result, err := strictjson.Boolean(value)
	if err != nil {
		return false, settingTypeError(field)
	}
	return result, nil
}

func settingTypeError(field string) error { return fmt.Errorf("setting %q has the wrong type", field) }

func parseIcons(value []byte) (map[string]string, error) {
	object, err := strictjson.Object(value)
	if err != nil {
		return nil, errors.New("icons must be an object")
	}
	icons := make(map[string]string, len(object))
	for id, raw := range object {
		if _, err := AgentByID(id); err != nil {
			return nil, errors.New("unknown icon agent")
		}
		icon, err := strictjson.String(raw)
		if err != nil {
			return nil, fmt.Errorf("icon override %q must be a string", id)
		}
		icons[id] = icon
	}
	return icons, nil
}

func ValidateSettings(settings Settings) error {
	if settings.Provider != "bark" && settings.Provider != "ntfy" {
		return errors.New("provider must be bark or ntfy")
	}
	if settings.Level != "critical" && settings.Level != "active" && settings.Level != "timeSensitive" && settings.Level != "passive" {
		return errors.New("invalid bark level")
	}
	if strings.TrimSpace(settings.Sound) == "" || !utf8.ValidString(settings.Sound) || len(settings.Sound) > maxDisplayBytes {
		return errors.New("sound must be a non-empty string up to 4096 UTF-8 bytes")
	}
	if settings.MinSeconds <= 0 || settings.LongTaskSeconds <= settings.MinSeconds {
		return errors.New("task thresholds must be positive and ordered")
	}
	if settings.MediumRingSeconds < 30 || settings.MediumRingSeconds > 60 || settings.LongRingSeconds < 30 || settings.LongRingSeconds > 60 {
		return errors.New("ring targets must be between 30 and 60 seconds")
	}
	if settings.Volume < 0 || settings.Volume > 10 {
		return errors.New("volume must be between 0 and 10")
	}
	if settings.NtfyPriority < 1 || settings.NtfyPriority > 5 {
		return errors.New("ntfy priority must be between 1 and 5")
	}
	if settings.Icons == nil {
		return errors.New("icons must be an object")
	}
	for id, icon := range settings.Icons {
		if _, err := AgentByID(id); err != nil {
			return errors.New("unknown icon agent")
		}
		if !utf8.ValidString(icon) || len(icon) > maxDisplayBytes {
			return errors.New("icon override must be at most 4096 UTF-8 bytes")
		}
	}
	return nil
}

func copySettings(settings Settings) Settings {
	icons := make(map[string]string, len(settings.Icons))
	for id, icon := range settings.Icons {
		icons[id] = icon
	}
	settings.Icons = icons
	return settings
}

func RingSeconds(settings Settings, durationSeconds int64) int {
	if durationSeconds < settings.MinSeconds {
		return 0
	}
	if durationSeconds >= settings.LongTaskSeconds {
		return settings.LongRingSeconds
	}
	return settings.MediumRingSeconds
}
