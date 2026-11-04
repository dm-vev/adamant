package remoteadmin

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/internal/httpauth"
)

// MinTokenLength is the length a token has at least.
const MinTokenLength = 32

// GeneratedTokenLength is the length of the token GenerateTokenFile makes,
// as Java's management secret: 40 letters and digits, 238 bits.
const GeneratedTokenLength = 40

// token is a token of the file of tokens: its name, its level and the hash
// of its secret. The secret itself is not kept.
type token struct {
	name  string
	level cmd.Level
	hash  httpauth.Hash
}

// readTokens reads the file of tokens: one a line, "<name> <level>
// <token>", blank lines and lines starting with # ignored. A token shorter
// than MinTokenLength, a level out of 0 to 4 and a name used twice are
// errors.
func readTokens(path string) ([]token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the remote admin tokens: %w", err)
	}
	var out []token
	names := map[string]bool{}
	s := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; s.Scan(); n++ {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: want <name> <level> <token>", path, n)
		}
		level, err := strconv.Atoi(fields[1])
		if err != nil || level < 0 || level > int(cmd.MaxLevel) {
			return nil, fmt.Errorf("%s:%d: the level of %s is %q: want 0 to 4", path, n, fields[0], fields[1])
		}
		if len(fields[2]) < MinTokenLength {
			// The token is not named: it may be a real one, too short.
			return nil, fmt.Errorf("%s:%d: the token of %s is shorter than %d characters", path, n, fields[0], MinTokenLength)
		}
		if names[fields[0]] {
			return nil, fmt.Errorf("%s:%d: a second token named %s", path, n, fields[0])
		}
		names[fields[0]] = true
		out = append(out, token{name: fields[0], level: cmd.Level(level), hash: httpauth.HashOf(fields[2])})
	}
	return out, s.Err()
}

// tokenAlphabet are the characters of a generated token.
const tokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// generateToken returns a token of GeneratedTokenLength characters of
// tokenAlphabet from crypto/rand, as Java's SecurityConfig.generateSecretKey.
func generateToken() (string, error) {
	var b strings.Builder
	n := big.NewInt(int64(len(tokenAlphabet)))
	for range GeneratedTokenLength {
		i, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", err
		}
		b.WriteByte(tokenAlphabet[i.Int64()])
	}
	return b.String(), nil
}

// GenerateTokenFile makes the file of tokens at path, if there is none,
// with mode 0600, holding one token, "owner", of level 4. It reports
// whether it made the file. The token is written to the file only: the
// caller must never log it.
func GenerateTokenFile(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	t, err := generateToken()
	if err != nil {
		return false, fmt.Errorf("generate a remote admin token: %w", err)
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return false, err
		}
	}
	data := "# Tokens of the remote admin API, one a line: <name> <level 0-4> <token>. Keep this file private.\n" +
		"owner 4 " + t + "\n"
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(data); err != nil {
		_ = f.Close()
		return false, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return false, err
	}
	return true, f.Close()
}

// TokenFileTooOpen reports whether the file of tokens at path may be read
// by its group or by others, which a server warns of, or refuses with
// StrictConfig.
func TokenFileTooOpen(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.Mode().Perm()&0o077 != 0, nil
}
