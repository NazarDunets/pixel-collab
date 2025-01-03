package room

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

const (
	COOKIE_USERNAME = "username"
)

type joinRequestPayload struct {
	ServerId string `json:"serverId"`
	Username string `json:"username"`
}

func PostJoin(c echo.Context) error {
	var requestPayload joinRequestPayload

	if err := c.Bind(&requestPayload); err != nil {
		c.String(http.StatusBadRequest, "Bad request")
	}

	if !roomExists(requestPayload.ServerId) {
		c.String(http.StatusNotFound, "Room doesn't exist")
	}

	username := requestPayload.Username + "#0000"
	usernameCookie := newSecureCookie(COOKIE_USERNAME, username, "/room/"+requestPayload.ServerId)
	c.SetCookie(usernameCookie)

	return c.NoContent(http.StatusOK)
}

func getCookieUsername(c echo.Context) (string, error) {
	cookie, err := c.Cookie(COOKIE_USERNAME)
	if err != nil {
		return "", err
	}
	if cookie.Value == "" {
		return "", errors.New("empty username")
	}
	return cookie.Value, nil
}

func roomExists(roomId string) bool {
	return roomStore[roomId] != nil
}

func newSecureCookie(name, value, path string) *http.Cookie {
	cookie := new(http.Cookie)
	cookie.Name = name
	cookie.Value = value
	cookie.SameSite = http.SameSiteLaxMode
	cookie.Path = path
	cookie.HttpOnly = true
	cookie.Secure = true
	return cookie
}