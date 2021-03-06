package handler

import (
	"github.com/grooveshark/golib/gslog"
	"github.com/kinghrothgar/gobin/storage/store"
	"github.com/kinghrothgar/gobin/templ"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	HORDE_MAX_LEN = 50
)

var (
	alphaReg            = regexp.MustCompile("^[A-Za-z]+$")
	alphaNumericReg     = regexp.MustCompile("^[A-Za-z0-9]+$")
	browserUserAgentReg = regexp.MustCompile("Mozilla")
	textContentTypeReg  = regexp.MustCompile("^text/")
	uidLen              int
	tokenLen            int
)

func getGobData(w http.ResponseWriter, r *http.Request) []byte {
	str := r.FormValue("gob")
	// TODO: maybe keep as string until we return it?
	return []byte(str)
}

func validToken(token string) bool {
	// prevents a lookup if it's obviously crap
	if len(token) > tokenLen || !alphaReg.MatchString(token) {
		return false
	}
	return true
}

func validUID(uid string) bool {
	// prevents a lookup if it's obviously crap
	if len(uid) > uidLen || !alphaReg.MatchString(uid) {
		return false
	}
	return true
}

func validHordeName(hordeName string) bool {
	// TODO: horde max length?
	if len(hordeName) > HORDE_MAX_LEN || !alphaNumericReg.MatchString(hordeName) {
		return false
	}
	return true
}

// Request.RemoteAddress contains port, which we want to remove i.e.:
// "[::1]:58292" => "[::1]"
func ipAddrFromRemoteAddr(s string) string {
	host, _, _ := net.SplitHostPort(s)
	return net.ParseIP(host).String()
}

func returnHTTPError(w http.ResponseWriter, funcName string, errMessage string, status int) {
	gslog.Debug("HANDLER: %s returned error to user: %s", funcName, errMessage)
	http.Error(w, "Error: "+errMessage, http.StatusInternalServerError)
}

func getIpAddress(r *http.Request) string {
	hdr := r.Header
	hdrRealIp := hdr.Get("X-Real-Ip")
	hdrForwardedFor := hdr.Get("X-Forwarded-For")
	if hdrRealIp == "" && hdrForwardedFor == "" {
		return ipAddrFromRemoteAddr(r.RemoteAddr)
	}
	if hdrForwardedFor != "" {
		// X-Forwarded-For is potentially a list of addresses separated with ","
		parts := strings.Split(hdrForwardedFor, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		// TODO: should return first non-local address
		return parts[0]
	}
	return net.ParseIP(hdrRealIp).String()
}

func getScheme(r *http.Request) (scheme string) {
	hdr := r.Header
	if scheme = hdr.Get("X-Real-Scheme"); scheme == "" {
		scheme = "http"
	}
	return
}

func getPageType(r *http.Request) string {
	userAgent := r.Header.Get("User-Agent")
	params := r.URL.Query()
	_, cli := params["cli"]
	// If cli param present or Mozilla not found in the user agent, use plain text
	if cli || !browserUserAgentReg.MatchString(userAgent) {
		return "TEXT"
	}
	return "HTML"
}

func getLanguage(r *http.Request) string {
	params := r.URL.Query()
	// Firgure out language parameter
	lang := ""
	for key, _ := range params {
		// Skip the pat params that start with :
		if key[0] == ':' {
			continue
		}
		lang = strings.ToLower(key)
	}
	// Deal with aliases
	switch lang {
	case "coffee":
		lang = "coffeescript"
		break
	case "groovy", "gvy", "gy", "gsh":
		lang = "groovy"
		break
	case "ruby", "rb":
		lang = "ruby"
		break
	case "markdown", "md":
		lang = "markdown"
		break
	}
	return lang
}

// Must be called before any other functions are called
// TODO: should I just called it SetUIDLen ?
func Initialize(uidLength int, tokenLength int) {
	uidLen = uidLength
	tokenLen = tokenLength
	gslog.Debug("HANDLER: initialized with uid length: %d, token length: %d", uidLen, tokenLen)
}

func GetRoot(w http.ResponseWriter, r *http.Request) {
	gslog.Debug("HANDLER: GetRoot called with header: %+v, host: %s, requestURI: %s, remoteAddr: %s", r.Header, r.Host, r.RequestURI, r.RemoteAddr)
	pageType := getPageType(r)
	pageBytes, err := templ.GetHomePage(pageType)
	if err != nil {
		gslog.Error("HANDLER: failed to get home with error: %s", err.Error())
		returnHTTPError(w, "GetRoot", "failed to get home", http.StatusInternalServerError)
		return
	}
	w.Write(pageBytes)
}

func GetForm(w http.ResponseWriter, r *http.Request) {
	gslog.Debug("HANDLER: GetForm called with header: %+v, host: %s, requestURI: %s, remoteAddr: %s", r.Header, r.Host, r.RequestURI, r.RemoteAddr)
	pageBytes, err := templ.GetFormPage(getScheme(r))
	if err != nil {
		gslog.Error("HANDLER: failed to get form with error: %s", err.Error())
		returnHTTPError(w, "GetRoot", "failed to get form", http.StatusInternalServerError)
		return
	}
	w.Write(pageBytes)
}

func GetGob(w http.ResponseWriter, r *http.Request) {
	gslog.Debug("HANDLER: GetGob called with header: %+v, host: %s, requestURI: %s, remoteAddr: %s", r.Header, r.Host, r.RequestURI, r.RemoteAddr)
	params := r.URL.Query()
	uid := params.Get(":uid")
	if !validUID(uid) {
		returnHTTPError(w, "GetGob", uid+" not found", http.StatusNotFound)
		return
	}
	data, _, err := store.GetGob(uid)
	if err != nil {
		gslog.Error("HANDLER: failed to get gob with error: " + err.Error())
		returnHTTPError(w, "GetGob", "failed to get gob", http.StatusInternalServerError)
		return
	}
	if len(data) == 0 {
		returnHTTPError(w, "GetGob", uid+" not found", http.StatusNotFound)
		return
	}
	// Firgure out language parameter
	lang := getLanguage(r)

	if lang == "" {
		gslog.Debug("HANDLER: GetGob writing data")
		w.Write(data)
		return
	}
	contentType := http.DetectContentType(data)
	// If data is a valid content type for syntax highlighting
	if textContentTypeReg.MatchString(contentType) {
		data, err = templ.GetGobPage(lang, data)
		if err != nil {
			gslog.Error("HANDLER: failed to get gob page with error: %s", err.Error())
			returnHTTPError(w, "GetGob", "failed to get gob", http.StatusInternalServerError)
			return
		}
	}

	w.Write(data)
}

func GetHorde(w http.ResponseWriter, r *http.Request) {
	gslog.Debug("HANDLER: GetHorde called with header: %+v, host: %s, requestURI: %s, remoteAddr: %s", r.Header, r.Host, r.RequestURI, r.RemoteAddr)
	params := r.URL.Query()
	hordeName := params.Get(":horde")
	if !validHordeName(hordeName) {
		returnHTTPError(w, "GetHorde", hordeName+" not found", http.StatusNotFound)
		return
	}
	horde, err := store.GetHorde(hordeName)
	if err != nil {
		gslog.Error("HANDLER: failed to get horde with error: %s", err.Error())
		returnHTTPError(w, "GetHorde", "failed to get horde", http.StatusInternalServerError)
		return
	}
	if len(horde) == 0 {
		returnHTTPError(w, "GetHorde", hordeName+" not found", http.StatusNotFound)
		return
	}
	pageType := getPageType(r)
	pageBytes, err := templ.GetHordePage(getScheme(r), pageType, hordeName, horde)
	if err != nil {
		gslog.Error("HANDLER: failed to get horde with error: %s", err.Error())
		returnHTTPError(w, "GetHorde", "failed to get horde", http.StatusInternalServerError)
		return
	}
	w.Write(pageBytes)
}

func PostGob(w http.ResponseWriter, r *http.Request) {
	gslog.Debug("HANDLER: PostGob called with header: %+v, host: %s, requestURI: %s, remoteAddr: %s", r.Header, r.Host, r.RequestURI, r.RemoteAddr)
	gobData := getGobData(w, r)
	if len(gobData) == 0 {
		returnHTTPError(w, "PostGob", "gob empty", http.StatusBadRequest)
		return
	}

	ip := getIpAddress(r)
	uid, token, err := store.PutGob(gobData, ip)
	gslog.Debug("HANDLER: PostGob uid: %s, ip: %s, token: %s", uid, ip, token)
	if err != nil {
		gslog.Error("HANDLER: post gob failed with error: %s", err.Error())
		returnHTTPError(w, "PostGob", "failed to save gob", http.StatusInternalServerError)
		return
	}

	pageType := getPageType(r)
	pageBytes, err := templ.GetURLPage(getScheme(r), pageType, uid, token)
	if err != nil {
		gslog.Error("HANDLER: post gob failed with error: %s", err.Error())
		returnHTTPError(w, "GetHorde", "failed to save gob", http.StatusInternalServerError)
		return
	}
	w.Write(pageBytes)
}

func AppendGob(w http.ResponseWriter, r *http.Request) {
	gslog.Debug("HANDLER: AppendGob called with header: %+v, host: %s, requestURI: %s, remoteAddr: %s", r.Header, r.Host, r.RequestURI, r.RemoteAddr)
	params := r.URL.Query()
	token := params.Get(":token")
	if !validToken(token) {
		returnHTTPError(w, "AppendGob", token+" not found", http.StatusNotFound)
		return
	}
	gobData := getGobData(w, r)
	if len(gobData) == 0 {
		returnHTTPError(w, "AppendGob", "gob empty", http.StatusBadRequest)
		return
	}
	ip := getIpAddress(r)
	uid, err := store.TokenToUID(token)
	gslog.Debug("HANDLER: AppendGob uid: %s, ip: %s, token: %s", uid, ip, token)
	if err != nil {
		gslog.Error("HANDLER: append gob failed with error: %s", err.Error())
		returnHTTPError(w, "AppendGob", "failed to append gob", http.StatusInternalServerError)
		return
	}
	if uid == "" {
		returnHTTPError(w, "AppendGob", token+" not found", http.StatusNotFound)
		return
	}
	err = store.AppendGob(uid, gobData)
	if err != nil {
		gslog.Error("HANDLER: append gob failed with error: %s", err.Error())
		returnHTTPError(w, "AppendGob", "failed to append gob", http.StatusInternalServerError)
		return
	}

	pageType := getPageType(r)
	// TODO: Should I tell them what gob they appended? Maybe a security flaw
	pageBytes, err := templ.GetMessPage(pageType, "successfully appended "+uid)
	w.Write(pageBytes)
}

func PostHordeGob(w http.ResponseWriter, r *http.Request) {
	gslog.Debug("HANDLER: PostHordeGob called with header: %+v, host: %s, requestURI: %s, remoteAddr: %s", r.Header, r.Host, r.RequestURI, r.RemoteAddr)
	params := r.URL.Query()
	hordeName := params.Get(":horde")
	if !validHordeName(hordeName) {
		returnHTTPError(w, "PostHordeGob", "horde name can only contain up to "+strconv.Itoa(HORDE_MAX_LEN)+" alphanumeric characters", http.StatusNotFound)
		return
	}
	gobData := getGobData(w, r)
	if len(gobData) == 0 {
		returnHTTPError(w, "PostHordeGob", "gob empty", http.StatusBadRequest)
		return
	}
	ip := getIpAddress(r)
	uid, token, err := store.PutHordeGob(hordeName, gobData, ip)
	gslog.Debug("HANDLER: uid: %s, ip: %s", uid, ip)
	if err != nil {
		gslog.Error("HANDLER: put horde gob failed with error: %s", err.Error())
		returnHTTPError(w, "PostHordeGob", "failed to save gob", http.StatusInternalServerError)
		return
	}

	pageType := getPageType(r)
	pageBytes, err := templ.GetURLPage(getScheme(r), pageType, uid, token)
	w.Write(pageBytes)
}

func DelGob(w http.ResponseWriter, r *http.Request) {
	gslog.Debug("HANDLER: DelGob called with header: %+v, host: %s, requestURI: %s, remoteAddr: %s", r.Header, r.Host, r.RequestURI, r.RemoteAddr)
	params := r.URL.Query()
	token := params.Get(":token")
	if !validToken(token) {
		returnHTTPError(w, "DelGob", token+" not found", http.StatusNotFound)
		return
	}
	uid, err := store.TokenToUID(token)
	if err != nil {
		gslog.Error("HANDLER: delete gob failed with error: %s", err.Error())
		returnHTTPError(w, "DelGob", "failed to delete gob", http.StatusInternalServerError)
		return
	}
	if uid == "" {
		returnHTTPError(w, "DelGob", token+" not found", http.StatusNotFound)
		return
	}
	err = store.DelGob(uid)
	if err != nil {
		gslog.Error("HANDLER: delete gob failed with error: %s", err.Error())
		returnHTTPError(w, "DelGob", "failed to delete gob", http.StatusInternalServerError)
		return
	}

	pageType := getPageType(r)
	pageBytes, err := templ.GetMessPage(pageType, "successfully deleted "+uid)
	w.Write(pageBytes)
}
