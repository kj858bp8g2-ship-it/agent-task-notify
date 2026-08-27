package secrets

import (
	"crypto/rand"

	keychain "github.com/keybase/go-keychain"
)

const keychainService = "agenttasknotify.native.v1"

func keychainItem(id string) keychain.Item {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(keychainService)
	item.SetAccount(id)
	item.SetSynchronizable(keychain.SynchronizableNo)
	return item
}

func readKey(id string) ([]byte, error) {
	item := keychainItem(id)
	item.SetMatchLimit(keychain.MatchLimitOne)
	item.SetReturnData(true)
	results, err := keychain.QueryItem(item)
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(results) == 0 {
		return nil, nil
	}
	if len(results) != 1 || len(results[0].Data) != 32 {
		for i := range results {
			clear(results[i].Data)
		}
		return nil, ErrUnavailable
	}
	return results[0].Data, nil
}

func openNative(id string, mode AccessMode) (protector, error) {
	var key []byte
	err := withInteraction(mode, func() error {
		var err error
		key, err = readKey(id)
		if err != nil {
			return ErrUnavailable
		}
		if len(key) == 32 {
			return nil
		}
		if mode == Background {
			return ErrUnavailable
		}
		candidate := make([]byte, 32)
		defer clear(candidate)
		if _, err = rand.Read(candidate); err != nil {
			return ErrUnavailable
		}
		item := keychainItem(id)
		item.SetData(candidate)
		err = keychain.AddItem(item)
		if err != nil && err != keychain.ErrorDuplicateItem {
			return ErrUnavailable
		}
		// Another process may have won creation. Never update/replace its DEK.
		key, err = readKey(id)
		if err != nil || len(key) != 32 {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		clear(key)
		return nil, ErrUnavailable
	}
	return &aesBackend{key: key}, nil
}
