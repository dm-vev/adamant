package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// Characters the names of players may hold: Latin letters, digits and
// spaces for players of Xbox Live, any letters for others.
var (
	onlineNameChar  = regexp.MustCompile("[A-Za-z0-9 ]").MatchString
	offlineNameChar = regexp.MustCompile(`[\p{L}0-9 ]`).MatchString
	versionChar     = regexp.MustCompile("[0-9.]").MatchString
)

// validateLogin holds the identity and the client data of a client that
// logged in to the rules the server keeps them to, those gophertunnel kept
// them to: a valid UUID and XUID, a name of the game's form, a device and a
// language that exist, skins of the sizes they claim. The address the
// client names the server by is read, not resolved: a lookup of a name a
// client chose would let it hold the login on a slow resolver.
func validateLogin(id login.Identity, data login.ClientData) error {
	if err := validateIdentity(id); err != nil {
		return err
	}
	return validateClientData(data)
}

func validateIdentity(data login.Identity) error {
	if _, err := strconv.ParseInt(data.XUID, 10, 64); err != nil && len(data.XUID) != 0 {
		return fmt.Errorf("XUID must be parseable as an int64, but got %v", data.XUID)
	}
	if id, err := uuid.Parse(data.Identity); err != nil || id == uuid.Nil {
		return fmt.Errorf("UUID must be parseable as a valid UUID, but got %v", data.Identity)
	}
	nameLimit := 15
	if data.XUID == "" {
		// Clients without an account may take one more character.
		nameLimit = 16
	}
	name := data.DisplayName
	if len(name) == 0 || len(name) > nameLimit {
		return fmt.Errorf("DisplayName must not be empty or longer than %d characters, but got %v characters", nameLimit, len(name))
	}
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return fmt.Errorf("DisplayName may not have a space as first/last character, but got %v", name)
	}
	if name[0] >= '0' && name[0] <= '9' {
		return fmt.Errorf("DisplayName may not have a number as first character, but got %v", name)
	}
	if data.XUID != "" {
		if !onlineNameChar(name) {
			return fmt.Errorf("DisplayName for authorized client must only contain numbers, Latin letters and spaces, but got %v", name)
		}
	} else if !offlineNameChar(name) {
		return fmt.Errorf("DisplayName for unauthorized client must only contain numbers, letters and spaces, but got %v", name)
	}
	if strings.Contains(name, "  ") {
		return fmt.Errorf("DisplayName must only have single spaces, but got %v", name)
	}
	return nil
}

func validateClientData(data login.ClientData) error {
	if data.DeviceOS <= 0 || data.DeviceOS > 15 {
		return fmt.Errorf("DeviceOS must carry a value between 1 and 15, but got %v", data.DeviceOS)
	}
	if !versionChar(data.GameVersion) {
		return fmt.Errorf("GameVersion must only contain dots and numbers, but got %v", data.GameVersion)
	}
	if _, err := language.Parse(strings.Replace(data.LanguageCode, "_", "-", 1)); err != nil {
		return fmt.Errorf("LanguageCode must be a valid BCP-47 ISO language code, but got %v", data.LanguageCode)
	}
	if _, err := uuid.Parse(data.PlatformOfflineID); err != nil && len(data.PlatformOfflineID) != 0 {
		return fmt.Errorf("PlatformOfflineID must be parseable as a valid UUID or empty, but got %v", data.PlatformOfflineID)
	}
	if _, err := strconv.ParseUint(data.PlatformOnlineID, 10, 64); err != nil && len(data.PlatformOnlineID) != 0 {
		return fmt.Errorf("PlatformOnlineID must be parseable as an int64 or empty, but got %v", data.PlatformOnlineID)
	}
	if _, err := uuid.Parse(data.SelfSignedID); data.SelfSignedID != "" && err != nil {
		return fmt.Errorf("SelfSignedID must be parseable as a valid UUID, but got %v", data.SelfSignedID)
	}
	if err := validateServerAddress(data.ServerAddress); err != nil {
		return err
	}
	if err := base64Length(data.SkinData, data.SkinImageHeight*data.SkinImageWidth*4); err != nil {
		return fmt.Errorf("SkinData is invalid: %w", err)
	}
	if err := base64Length(data.CapeData, data.CapeImageHeight*data.CapeImageWidth*4); err != nil {
		return fmt.Errorf("CapeData is invalid: %w", err)
	}
	for _, c := range data.PlayFabID {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("PlayFabID must consist of hex characters, got %v", data.PlayFabID)
		}
	}
	for _, anim := range data.AnimatedImageData {
		if err := base64Length(anim.Image, anim.ImageHeight*anim.ImageWidth*4); err != nil {
			return fmt.Errorf("invalid animated image data: %w", err)
		}
		if anim.Type < 0 || anim.Type > 3 {
			return fmt.Errorf("invalid animation type: %v", anim.Type)
		}
	}
	if geometry, err := base64.StdEncoding.DecodeString(data.SkinGeometry); err != nil {
		return fmt.Errorf("SkinGeometry was not a valid base64 string: %w", err)
	} else if len(geometry) != 0 {
		var m map[string]any
		if err := json.Unmarshal(geometry, &m); err != nil {
			return fmt.Errorf("SkinGeometry base64 decoded was not a valid JSON string: %w", err)
		}
	}
	patch, err := base64.StdEncoding.DecodeString(data.SkinResourcePatch)
	if err != nil {
		return fmt.Errorf("SkinResourcePatch was not a valid base64 string: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(patch, &m); err != nil {
		return fmt.Errorf("SkinResourcePatch base64 decoded was not a valid JSON string: %w", err)
	}
	if data.SkinID == "" {
		return fmt.Errorf("SkinID must not be an empty string")
	}
	if data.UIProfile < 0 || data.UIProfile > 2 {
		return fmt.Errorf("UIProfile must be between 0-2, but got %v", data.UIProfile)
	}
	return nil
}

// validateServerAddress checks the address a client names the server by:
// a URL of the signaling with the port after it, or a host and a port.
func validateServerAddress(addr string) error {
	if strings.Contains(addr, "://") {
		i := strings.LastIndex(addr, ":")
		u, err := url.Parse(addr[:i])
		if err != nil {
			return fmt.Errorf("ServerAddress must be a URL, but got %v", addr)
		}
		if u.Host == "" || u.Port() == "" || u.Port() != addr[i+1:] || u.Scheme != "https" && u.Scheme != "http" {
			return fmt.Errorf("ServerAddress is invalid: %v", addr)
		}
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("ServerAddress must be a host and a port, but got %v", addr)
	}
	if p, err := strconv.ParseUint(port, 10, 16); err != nil || p == 0 && port != "0" {
		return fmt.Errorf("ServerAddress has an invalid port: %v", addr)
	}
	if strings.ContainsAny(host, " /\\\x00") {
		return fmt.Errorf("ServerAddress has an invalid host: %v", addr)
	}
	return nil
}

// base64Length decodes base64 data and checks it is of the length passed.
func base64Length(data string, length int) error {
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("decode base64 data: %w", err)
	}
	if len(b) != length {
		return fmt.Errorf("invalid size: got %v, expected one of %v", len(b), []int{length})
	}
	return nil
}
