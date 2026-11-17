// Binary VA: 0x101c0d1c0 | Size: 21072 B (63 funcs) | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go
package codeassistclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zalando/go-keyring" /* inferred */
	"golang.org/x/oauth2"
	"google3/base/go/log"
)

const (
	keyringTimeout         = 5 * time.Second // 0x12a05f200 ns at 0x10ab199b0
	keyringFailureCooldown = time.Hour       // 0x034630b8a000 ns
)

// Binary VA: 0x10a2fc4a0 | Size: 24 B | Type: codeassistclient.storedTokenResult
type storedTokenResult struct {
	stored *StoredToken
	err    error
}

type loadResult struct {
	token       *oauth2.Token
	projectID   string
	region      string
	userTier    string
	displayName string
	err         error
}

// Binary VA: 0x10a7b6178 | Size: 88 B | Type: codeassistclient.compositeTokenStorage
type compositeTokenStorage struct {
	keyringStorage        TokenStorage
	fileStorage           TokenStorage
	mu                    sync.RWMutex
	useFileStatic         bool
	keyringWritesDisabled bool
	markerPath            string
	onKeyringError        func(string)
}

// Binary VA: 0x101c0d1c0 | Size: 240 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L114
func (c *compositeTokenStorage) isUseFileStatic() bool {
	c.mu.RLock()
	// Binary VA: 0x101c48c80 | Size: 128 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L116
	defer c.mu.RUnlock()
	return c.useFileStatic
}

// Binary VA: 0x101c0d2b0 | Size: 160 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L120
func (c *compositeTokenStorage) setUseFileStatic(v bool) {
	c.mu.Lock()
	// Binary VA: 0x101c48d00 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L122
	defer c.mu.Unlock()
	c.useFileStatic = v
}

// Binary VA: 0x101c0d350 | Size: 240 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L126
func (c *compositeTokenStorage) areKeyringWritesDisabled() bool {
	c.mu.RLock()
	// Binary VA: 0x101c48d40 | Size: 128 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L128
	defer c.mu.RUnlock()
	return c.keyringWritesDisabled
}

// Binary VA: 0x101c0d440 | Size: 336 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L135
func (c *compositeTokenStorage) disableKeyringWrites(op string, err error) {
	c.mu.Lock()
	prev := c.keyringWritesDisabled
	c.keyringWritesDisabled = true
	c.mu.Unlock()
	if !prev {
		log.ctxlogf(context.Background() /* inferred */, "Keyring %s failed (%v); using file-based token storage for writes for the rest of this process", op, err)
	}
}

// Binary VA: 0x101c0d590 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L150
func (c *compositeTokenStorage) useFileIfKeyringWritesDisabled() {
	if c.areKeyringWritesDisabled() {
		c.setUseFileStatic(true)
	}
}

// Binary VA: 0x101c0d5e0 | Size: 640 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L167
func NewCompositeTokenStorage(keyringStorage TokenStorage, fileStorage TokenStorage, forceFile bool, markerPath string, onKeyringError func(string)) TokenStorage {
	useFile := false
	if forceFile {
		log.ctxlogf(context.Background() /* inferred */, "Using file-based token storage because the caller requested it")
		useFile = true
	} else {
		bypass, reason := shouldBypassKeyring()
		if bypass {
			log.ctxlogf(context.Background() /* inferred */, "Using file-based token storage because %s detected", reason)
			useFile = true
		} else if markerPath != "" && keyringRecentlyUnavailable(markerPath) {
			log.ctxlogf(context.Background() /* inferred */, "Using file-based token storage because a keyring timeout was recorded within the last %v", keyringFailureCooldown)
			useFile = true
		}
	}
	return &compositeTokenStorage{
		keyringStorage: keyringStorage,
		fileStorage:    fileStorage,
		useFileStatic:  useFile,
		markerPath:     markerPath,
		onKeyringError: onKeyringError,
	}
}

// Binary VA: 0x101c0d860 | Size: 336 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L196
func DefaultKeyringMarkerPath(appDataDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.ctxlogf(context.Background() /* inferred */, "Failed to get user home directory for keyring marker: %v", err)
		return ""
	}
	return filepath.Join(home, ".gemini", "cache", appDataDir+"-keyring-unavailable")
}

// Binary VA: 0x101c0d9b0 | Size: 608 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L208
func keyringRecentlyUnavailable(markerPath string) bool {
	info, err := os.Stat(markerPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.ctxlogf(context.Background() /* inferred */, "failed to stat keyring timeout marker %s: %v", markerPath, err)
		}
		return false
	}
	if time.Since(info.ModTime()) < keyringFailureCooldown {
		return true
	}
	if err := os.Remove(markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.ctxlogf(context.Background() /* inferred */, "failed to remove stale keyring timeout marker %s: %v", markerPath, err)
	}
	return false
}

// Binary VA: 0x101c0dc10 | Size: 1376 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L228
func (c *compositeTokenStorage) memoizeKeyringTimeout() {
	if c.markerPath != "" {
		dir := filepath.Dir(c.markerPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.ctxlogf(context.Background() /* inferred */, "failed to create keyring marker directory: %v", err)
			return
		}
		tmp, err := os.CreateTemp(dir, "keyring-marker-*")
		if err != nil {
			log.ctxlogf(context.Background() /* inferred */, "failed to create temporary keyring marker file: %v", err)
			return
		}
		tmpName := tmp.Name()
		if _, err := tmp.Write([]byte(time.Now().Format("2006-01-02T15:04:05Z07:00"))); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			log.ctxlogf(context.Background() /* inferred */, "failed to write keyring marker: %v", err)
			return
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpName)
			log.ctxlogf(context.Background() /* inferred */, "failed to close temporary keyring marker file: %v", err)
			return
		}
		if err := os.Chmod(tmpName, 0o600); err != nil {
			_ = os.Remove(tmpName)
			log.ctxlogf(context.Background() /* inferred */, "failed to set permissions on temporary keyring marker file: %v", err)
			return
		}
		if err := os.Rename(tmpName, c.markerPath); err != nil {
			_ = os.Remove(tmpName)
			log.ctxlogf(context.Background() /* inferred */, "failed to rename keyring marker file: %v", err)
			return
		}
	}
	c.setUseFileStatic(true)
}

// Binary VA: 0x101c0e170 | Size: 480 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L275
func (c *compositeTokenStorage) SaveToken(tok *oauth2.Token, authMethod string, opts ...SaveTokenOption) error {
	if c.isUseFileStatic() {
		return c.fileStorage.SaveToken(tok, authMethod, opts...)
	}
	fileStored, err := c.fileStorage.LoadStoredToken()
	if err == nil && fileStored != nil && !fileStored.IsEmpty() {
		return c.saveTokenWithExistingFileState(fileStored, tok, authMethod, opts...)
	}
	// Binary VA: 0x101c48dc0 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L283
	err = c.writeKeyring("SaveToken", "save", func() error {
		return c.keyringStorage.SaveToken(tok, authMethod, opts...)
	})
	if err != nil {
		return c.fallbackSaveTokenToFile(tok, authMethod, opts...)
	}
	return nil
}

// Binary VA: 0x101c0e350 | Size: 1648 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L303
func (c *compositeTokenStorage) LoadToken() (*oauth2.Token, string, string, string, string, error) {
	if c.isUseFileStatic() {
		return c.fileStorage.LoadToken()
	}
	ch := make(chan loadResult, 1)
	// Binary VA: 0x101c48e50 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L310
	go func() {
		t, pid, reg, tier, disp, err := c.keyringStorage.LoadToken()
		ch <- loadResult{t, pid, reg, tier, disp, err}
	}()
	timer := time.NewTimer(keyringTimeout)
	// Binary VA: 0x101c48e10 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L316
	defer timer.Stop()

	select {
	case res := <-ch:
		if res.err == nil && res.token != nil {
			healed := c.healKeyringTokenFromFile(&StoredToken{
				Token:           res.token,
				ProjectID:       res.projectID,
				Region:          res.region,
				UserTier:        res.userTier,
				TierDisplayName: res.displayName,
			})
			return healed.Token, healed.ProjectID, healed.Region, healed.UserTier, healed.TierDisplayName, nil
		}
		if res.err != nil {
			log.ctxlogf(context.Background() /* inferred */, "Failed to load token from keyring, falling back to file: %v", res.err)
			if c.onKeyringError != nil {
				c.onKeyringError("load")
			}
		} else if res.projectID != "" || res.region != "" || res.userTier != "" {
			/* inferred: tokenless keyring metadata */
			_ = &StoredToken{
				ProjectID:       res.projectID,
				Region:          res.region,
				UserTier:        res.userTier,
				TierDisplayName: res.displayName,
			}
		}
	case <-timer.C:
		log.ctxlogf(context.Background() /* inferred */, "Keyring LoadToken timed out after %v, falling back to file storage", keyringTimeout)
		if c.onKeyringError != nil {
			c.onKeyringError("load")
		}
		c.memoizeKeyringTimeout()
	}

	tok, pid, reg, tier, disp, err := c.fileStorage.LoadToken()
	if err == nil && tok != nil {
		/* inferred: merge metadata if file token has gcp auth and missing project/tier */
		return tok, pid, reg, tier, disp, nil
	}
	return tok, pid, reg, tier, disp, err
}

// Binary VA: 0x101c0e9c0 | Size: 256 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L362
func (c *compositeTokenStorage) SaveUserTier(userTier, displayName string) error {
	// Binary VA: 0x101c48f10 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L366
	keyringFn := func() error {
		return c.keyringStorage.SaveUserTier(userTier, displayName)
	}
	// Binary VA: 0x101c48ec0 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L367
	fileFn := func() error {
		return c.fileStorage.SaveUserTier(userTier, displayName)
	}
	return c.saveMetadata("SaveUserTier", "save_user_tier", keyringFn, fileFn)
}

// Binary VA: 0x101c0eac0 | Size: 544 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L375
func (c *compositeTokenStorage) RemoveToken() error {
	if c.isUseFileStatic() {
		return c.fileStorage.RemoveToken()
	}
	// Binary VA: 0x101c48f60 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L380
	keyringErr := c.runKeyringWrite("RemoveToken", "remove", func() error {
		return c.keyringStorage.RemoveToken()
	})
	fileErr := c.fileStorage.RemoveToken()
	if keyringErr != nil && fileErr != nil {
		return fmt.Errorf("failed to remove token from both keyring (%v) and file (%v)", keyringErr, fileErr)
	}
	if fileErr != nil {
		log.ctxlogf(context.Background() /* inferred */, "Failed to remove token from file: %v", fileErr)
	}
	return nil
}

// Binary VA: 0x101c0ece0 | Size: 256 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L397
func (c *compositeTokenStorage) SaveProjectAndRegion(projectID, region string) error {
	// Binary VA: 0x101c48ff0 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L401
	keyringFn := func() error {
		return c.keyringStorage.SaveProjectAndRegion(projectID, region)
	}
	// Binary VA: 0x101c48fa0 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L402
	fileFn := func() error {
		return c.fileStorage.SaveProjectAndRegion(projectID, region)
	}
	return c.saveMetadata("SaveProjectAndRegion", "save_project_region", keyringFn, fileFn)
}

// Binary VA: 0x101c0ede0 | Size: 256 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L415
func (c *compositeTokenStorage) LoadStoredToken() (*StoredToken, error) {
	if c.isUseFileStatic() {
		return c.fileStorage.LoadStoredToken()
	}
	keyringStored, ok, keyringHasSignOut := c.loadFromKeyring()
	if ok && keyringStored != nil && keyringStored.Token != nil {
		return c.healKeyringTokenFromFile(keyringStored), nil
	}
	fileStored, err := c.fileStorage.LoadStoredToken()
	if err == nil && fileStored != nil && fileStored.Token != nil {
		return mergeMissingFromTokenlessKeyring(fileStored, keyringStored), nil
	}
	if keyringHasSignOut {
		return mergeSignOutState(fileStored, keyringStored), nil
	}
	return fileStored, err
}

// Binary VA: 0x101c0eee0 | Size: 464 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L440
func (c *compositeTokenStorage) UpdateStoredToken(fn func(*StoredToken)) error {
	if c.isUseFileStatic() || c.shouldUpdateFileDirectly() {
		if u, ok := c.fileStorage.(StoredTokenUpdater); ok {
			return u.UpdateStoredToken(fn)
		}
		return errors.New("token storage does not support UpdateStoredToken" /* inferred */)
	}
	// Binary VA: 0x101c49100 | Size: 192 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L444
	err := c.writeKeyring("UpdateStoredToken", "update_stored", func() error {
		if u, ok := c.keyringStorage.(StoredTokenUpdater); ok {
			return u.UpdateStoredToken(fn)
		}
		return errors.New("token storage does not support UpdateStoredToken" /* inferred */)
	})
	if err != nil {
		// Binary VA: 0x101c49040 | Size: 192 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L450
		return c.migrateKeyringAndApplyFileMetadata(true, func() error {
			if u, ok := c.fileStorage.(StoredTokenUpdater); ok {
				return u.UpdateStoredToken(fn)
			}
			return errors.New("token storage does not support UpdateStoredToken" /* inferred */)
		})
	}
	return nil
}

// Binary VA: 0x101c0f0b0 | Size: 1456 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L463
func writeStoredTokenTo(dst TokenStorage, st *StoredToken) error {
	if u, ok := dst.(StoredTokenUpdater); ok {
		// Binary VA: 0x101c49280 | Size: 160 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L465
		return u.UpdateStoredToken(func(cur *StoredToken) {
			*cur = *st
		})
	}
	if err := dst.RemoveToken(); err != nil {
		return err
	}
	tok := tokenForNonUpdaterWrite(st)
	var opts []SaveTokenOption
	opts = append(opts, WithWIFProvider(st.WIFProvider))
	if st.WriterID != "" {
		opts = append(opts, WithWriterID(st.WriterID))
	}
	if st.Token == nil && st.SavedWIFProvider != "" {
		if err := dst.SaveToken(tok, st.AuthMethod, WithWIFProvider(st.SavedWIFProvider)); err != nil {
			return err
		}
	} else {
		if err := dst.SaveToken(tok, st.AuthMethod, opts...); err != nil {
			return err
		}
	}
	if st.AuthMethod == "consumer" || st.ProjectID != "" || st.Region != "" {
		if err := dst.SaveProjectAndRegion(st.ProjectID, st.Region); err != nil {
			return err
		}
	}
	if st.AuthMethod == "consumer" || st.UserTier != "" || st.TierDisplayName != "" {
		if err := dst.SaveUserTier(st.UserTier, st.TierDisplayName); err != nil {
			return err
		}
	}
	return nil
}

// Binary VA: 0x101c0f660 | Size: 624 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L502
func tokenForNonUpdaterWrite(st *StoredToken) *oauth2.Token {
	if st.Token != nil {
		if raw, _ := st.Token.Extra("id_token").(string); raw != "" {
			return st.Token
		}
	}
	if st.IDTokenEmail == "" {
		if st.Token == nil {
			return nil
		}
		return st.Token
	}
	claimsBytes, err := json.Marshal(OIDCClaims{Email: st.IDTokenEmail})
	if err != nil {
		return st.Token
	}
	jwt := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claimsBytes) + "."
	var cloned oauth2.Token
	if st.Token != nil {
		cloned = *st.Token
	}
	return cloned.WithExtra(map[string]any{"id_token": jwt})
}

// Binary VA: 0x101c0f8d0 | Size: 224 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L525
func (c *compositeTokenStorage) writeKeyring(opName, metricOp string, fn func() error) error {
	if c.areKeyringWritesDisabled() {
		return keyring.ErrUnsupportedPlatform /* inferred: disabled sentinel */
	}
	err := c.runKeyringWrite(opName, metricOp, fn)
	if err != nil && isKeyringUnavailableWriteError(err) {
		c.disableKeyringWrites(opName, err)
	}
	return err
}

// Binary VA: 0x101c0f9b0 | Size: 112 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L541
func isKeyringUnavailableWriteError(err error) bool {
	return errors.Is(err, keyring.ErrUnsupportedPlatform) || errors.Is(err, keyring.ErrNotFound /* inferred */)
}

// Binary VA: 0x101c0fa20 | Size: 1024 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L545
func (c *compositeTokenStorage) runKeyringWrite(opName, metricOp string, fn func() error) error {
	ch := make(chan error, 1)
	// Binary VA: 0x101c49360 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L547
	go func() {
		ch <- fn()
	}()
	timer := time.NewTimer(keyringTimeout)
	// Binary VA: 0x101c49320 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L552
	defer timer.Stop()

	select {
	case err := <-ch:
		if err != nil {
			log.ctxlogf(context.Background() /* inferred */, "Failed keyring %s, falling back to file when applicable: %v", opName, err)
			if c.onKeyringError != nil {
				c.onKeyringError(metricOp)
			}
		}
		return err
	case <-timer.C:
		log.ctxlogf(context.Background() /* inferred */, "Keyring %s timed out after %v", opName, keyringTimeout)
		if c.onKeyringError != nil {
			c.onKeyringError(metricOp)
		}
		c.memoizeKeyringTimeout()
		return fmt.Errorf("keyring %s timed out after %v", opName, keyringTimeout)
	}
}

// Binary VA: 0x101c0fe20 | Size: 848 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L571
func (c *compositeTokenStorage) loadFromKeyring() (*StoredToken, bool, bool) {
	ch := make(chan storedTokenResult, 1)
	// Binary VA: 0x101c493f0 | Size: 96 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L573
	go func() {
		st, err := c.keyringStorage.LoadStoredToken()
		ch <- storedTokenResult{stored: st, err: err}
	}()
	timer := time.NewTimer(keyringTimeout)
	// Binary VA: 0x101c493b0 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L579
	defer timer.Stop()

	select {
	case res := <-ch:
		if res.err == nil && res.stored != nil && !res.stored.IsEmpty() {
			return res.stored, true, false
		}
		if res.err != nil {
			log.ctxlogf(context.Background() /* inferred */, "Failed to load stored token from keyring, falling back to file: %v", res.err)
			if c.onKeyringError != nil {
				c.onKeyringError("load_stored")
			}
		}
		return nil, false, false
	case <-timer.C:
		log.ctxlogf(context.Background() /* inferred */, "Keyring LoadStoredToken timed out after %v, falling back to file storage", keyringTimeout)
		if c.onKeyringError != nil {
			c.onKeyringError("load_stored")
		}
		c.memoizeKeyringTimeout()
		return nil, false, false
	}
}

// Binary VA: 0x101c10170 | Size: 464 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L599
func (c *compositeTokenStorage) peekKeyringStoredToken() (*StoredToken, bool) {
	ch := make(chan storedTokenResult, 1)
	// Binary VA: 0x101c49490 | Size: 96 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L601
	go func() {
		st, err := c.keyringStorage.LoadStoredToken()
		ch <- storedTokenResult{stored: st, err: err}
	}()
	timer := time.NewTimer(keyringTimeout)
	// Binary VA: 0x101c49450 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L607
	defer timer.Stop()

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, true
		}
		return res.stored, false
	case <-timer.C:
		c.memoizeKeyringTimeout()
		return nil, true
	}
}

// Binary VA: 0x101c10340 | Size: 224 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L620
func purgeTokenStorage(ts TokenStorage) error {
	if u, ok := ts.(StoredTokenUpdater); ok {
		// Binary VA: 0x101c50170 | Size: 144 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L621
		if err := u.UpdateStoredToken(func(s *StoredToken) {
			*s = StoredToken{}
		}); err == nil {
			return nil
		}
	}
	return ts.RemoveToken()
}

// Binary VA: 0x101c10420 | Size: 608 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L627
func (c *compositeTokenStorage) removeKeyringQuietly() {
	ch := make(chan error, 1)
	// Binary VA: 0x101c49530 | Size: 80 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L629
	go func() {
		ch <- purgeTokenStorage(c.keyringStorage)
	}()
	timer := time.NewTimer(keyringTimeout)
	// Binary VA: 0x101c494f0 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L634
	defer timer.Stop()

	select {
	case err := <-ch:
		if err != nil {
			log.ctxlogf(context.Background() /* inferred */, "Failed to clear stale keyring credential after file fallback: %v", err)
		}
	case <-timer.C:
		log.ctxlogf(context.Background() /* inferred */, "Keyring RemoveToken timed out after %v while clearing stale credential", keyringTimeout)
		c.memoizeKeyringTimeout()
	}
}

// Binary VA: 0x101c10680 | Size: 560 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L646
func (c *compositeTokenStorage) fallbackSaveTokenToFile(tok *oauth2.Token, authMethod string, opts ...SaveTokenOption) error {
	keyringStored, timedOut := c.peekKeyringStoredToken()
	if timedOut {
		keyringStored = nil
	}
	fileStored, err := c.fileStorage.LoadStoredToken()
	migrated := false
	if err == nil && keyringStored != nil && !keyringStored.IsEmpty() {
		if merged := healSplitKeyringToken(keyringStored, fileStored); merged != nil {
			if err := writeStoredTokenTo(c.fileStorage, merged); err == nil {
				migrated = true
			}
		}
	}
	if err := c.fileStorage.SaveToken(tok, authMethod, opts...); err != nil {
		if migrated {
			_ = restoreFileStorageAfterFailedMigration(c.fileStorage, fileStored)
		}
		return err
	}
	if !timedOut && keyringStored != nil && !keyringStored.IsEmpty() {
		c.removeKeyringQuietly()
	}
	c.useFileIfKeyringWritesDisabled()
	return nil
}

// Binary VA: 0x101c108b0 | Size: 752 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L675
func (c *compositeTokenStorage) saveTokenWithExistingFileState(fileStored *StoredToken, tok *oauth2.Token, authMethod string, opts ...SaveTokenOption) error {
	keyringStored, timedOut := c.peekKeyringStoredToken()
	if timedOut {
		if c.onKeyringError != nil {
			c.onKeyringError("save")
		}
		return c.fileStorage.SaveToken(tok, authMethod, opts...)
	}
	base := resolveSaveTokenBase(fileStored, keyringStored)
	cfg := &SaveTokenConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	base.ApplyToken(tok, authMethod, cfg)
	// Binary VA: 0x101c49580 | Size: 64 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L688
	err := c.writeKeyring("SaveToken", "save", func() error {
		return writeStoredTokenTo(c.keyringStorage, &base)
	})
	if err == nil {
		_ = purgeTokenStorage(c.fileStorage)
		return nil
	}
	if err := writeStoredTokenTo(c.fileStorage, &base); err != nil {
		return err
	}
	if !timedOut && keyringStored != nil && !keyringStored.IsEmpty() {
		c.removeKeyringQuietly()
	}
	c.useFileIfKeyringWritesDisabled()
	return nil
}

// Binary VA: 0x101c10ba0 | Size: 368 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L707
func resolveSaveTokenBase(fileStored, keyringStored *StoredToken) StoredToken {
	if keyringStored != nil && keyringStored.Token != nil {
		return *healSplitKeyringToken(keyringStored, fileStored)
	}
	if fileStored != nil && fileStored.Token != nil {
		return *mergeMissingFromTokenlessKeyring(fileStored, keyringStored)
	}
	return keptSignOutState(fileStored, keyringStored)
}

// Binary VA: 0x101c10d10 | Size: 336 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L717
func keptSignOutState(fileStored, keyringStored *StoredToken) StoredToken {
	var base StoredToken
	if keyringStored != nil {
		base, _ = keyringStored.KeptOnSignOut()
	}
	return *mergeSignOutState(&base, fileStored)
}

// Binary VA: 0x101c10e60 | Size: 416 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L725
func mergeSignOutState(dst, src *StoredToken) *StoredToken {
	if dst == nil || src == nil {
		return dst
	}
	kept, ok := src.KeptOnSignOut()
	if !ok {
		return dst
	}
	out := *dst
	if out.SavedWIFProvider == "" && kept.SavedWIFProvider != "" {
		out.SavedWIFProvider = kept.SavedWIFProvider
	}
	if out.AICodeScopeSignOutAttemptedAt == 0 && kept.AICodeScopeSignOutAttemptedAt != 0 {
		out.AICodeScopeSignOutAttemptedAt = kept.AICodeScopeSignOutAttemptedAt
	}
	return &out
}

// Binary VA: 0x101c11000 | Size: 208 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L743
func (c *compositeTokenStorage) saveMetadata(opName, metricOp string, keyringFn, fileFn func() error) error {
	if c.isUseFileStatic() || c.fileOwnsActiveToken() {
		return fileFn()
	}
	if err := c.writeKeyring(opName, metricOp, keyringFn); err != nil {
		return c.migrateKeyringAndApplyFileMetadata(false, fileFn)
	}
	return nil
}

// Binary VA: 0x101c110d0 | Size: 160 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L754
func (c *compositeTokenStorage) fileOwnsActiveToken() bool {
	fileStored, err := c.fileStorage.LoadStoredToken()
	if err != nil || fileStored == nil || fileStored.Token == nil {
		return false
	}
	keyringStored, timedOut := c.peekKeyringStoredToken()
	if timedOut {
		return true
	}
	return keyringStored == nil || keyringStored.Token == nil
}

// Binary VA: 0x101c11170 | Size: 240 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L766
func (c *compositeTokenStorage) shouldUpdateFileDirectly() bool {
	keyringStored, ok, _ := c.loadFromKeyring()
	if !ok {
		return true
	}
	if keyringStored.Token != nil {
		return false
	}
	fileStored, err := c.fileStorage.LoadStoredToken()
	if err != nil || fileStored == nil || fileStored.IsEmpty() {
		return false
	}
	return true
}

// Binary VA: 0x101c11260 | Size: 528 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L778
func (c *compositeTokenStorage) migrateKeyringAndApplyFileMetadata(preserveOnFail bool, fileFn func() error) error {
	keyringStored, timedOut := c.peekKeyringStoredToken()
	if timedOut {
		keyringStored = nil
	}
	fileStored, err := c.fileStorage.LoadStoredToken()
	migrated := false
	if err == nil && keyringStored != nil && !keyringStored.IsEmpty() {
		if merged := c.keyringStateToMigrate(keyringStored, fileStored); merged != nil {
			if err := writeStoredTokenTo(c.fileStorage, merged); err == nil {
				migrated = true
			}
		}
	}
	if err := fileFn(); err != nil {
		if migrated && !preserveOnFail {
			_ = restoreFileStorageAfterFailedMigration(c.fileStorage, fileStored)
		}
		return err
	}
	if !timedOut && keyringStored != nil && !keyringStored.IsEmpty() {
		c.removeKeyringQuietly()
	}
	c.useFileIfKeyringWritesDisabled()
	return nil
}

// Binary VA: 0x101c11470 | Size: 208 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L812
func (c *compositeTokenStorage) keyringStateToMigrate(keyringStored, fileStored *StoredToken) *StoredToken {
	if c.areKeyringWritesDisabled() && keyringStored.Token != nil {
		if fileStored == nil || fileStored.Token == nil || keyringStored.Token.Expiry.After(fileStored.Token.Expiry) {
			return mergeMissingFromTokenlessKeyring(fileStored, keyringStored)
		}
	}
	return healSplitKeyringToken(keyringStored, fileStored)
}

// Binary VA: 0x101c11540 | Size: 208 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L820
func restoreFileStorageAfterFailedMigration(fileStorage TokenStorage, prev *StoredToken) error {
	if prev == nil || prev.IsEmpty() {
		return purgeTokenStorage(fileStorage)
	}
	return writeStoredTokenTo(fileStorage, prev)
}

// Binary VA: 0x101c11610 | Size: 192 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L831
func (c *compositeTokenStorage) healKeyringTokenFromFile(keyringStored *StoredToken) *StoredToken {
	if keyringStored == nil || keyringStored.AuthMethod != "gcp" || (keyringStored.ProjectID != "" && keyringStored.UserTier != "") {
		return keyringStored
	}
	fileStored, err := c.fileStorage.LoadStoredToken()
	if err != nil {
		return keyringStored
	}
	return healSplitKeyringToken(keyringStored, fileStored)
}

// Binary VA: 0x101c116d0 | Size: 512 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L842
func healSplitKeyringToken(keyringStored, fileStored *StoredToken) *StoredToken {
	if keyringStored == nil || fileStored == nil || fileStored.Token != nil {
		return keyringStored
	}
	if keyringStored.AuthMethod != "gcp" || (keyringStored.ProjectID != "" && keyringStored.UserTier != "") {
		return keyringStored
	}
	out := *keyringStored
	if out.ProjectID == "" && fileStored.ProjectID != "" {
		out.ProjectID = fileStored.ProjectID
		if out.Region == "" {
			out.Region = fileStored.Region
		}
	}
	if out.UserTier == "" && fileStored.UserTier != "" && out.ProjectID == fileStored.ProjectID {
		out.UserTier = fileStored.UserTier
		out.TierDisplayName = fileStored.TierDisplayName
	}
	return &out
}

// Binary VA: 0x101c118d0 | Size: 560 B | Source: third_party/jetski/language_server/code_assist_client/composite_token_storage.go:L855
func mergeMissingFromTokenlessKeyring(fileStored, keyringStored *StoredToken) *StoredToken {
	if keyringStored == nil || keyringStored.IsEmpty() {
		return fileStored
	}
	out := *mergeSignOutState(fileStored, keyringStored)
	if out.AuthMethod != "gcp" {
		return &out
	}
	if out.ProjectID == "" && keyringStored.ProjectID != "" {
		out.ProjectID = keyringStored.ProjectID
		if out.Region == "" {
			out.Region = keyringStored.Region
		}
	}
	if out.UserTier == "" && keyringStored.UserTier != "" && out.ProjectID == keyringStored.ProjectID {
		out.UserTier = keyringStored.UserTier
		out.TierDisplayName = keyringStored.TierDisplayName
	}
	return &out
}
