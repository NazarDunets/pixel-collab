package room

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

const (
	COOKIE_USERNAME           = "username"
	COOKIE_REQUESTED_USERNAME = "requestedUsername"
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

	// TODO: validate username, validate roomId
	username := requestPayload.Username

	getOrCreateRoom(requestPayload.ServerId)

	usernameCookie := newCookie(COOKIE_REQUESTED_USERNAME, username, "/room/"+requestPayload.ServerId)
	c.SetCookie(usernameCookie)

	return c.NoContent(http.StatusOK)
}

func getNonEmptyCookie(c echo.Context, name string) (string, error) {
	cookie, err := c.Cookie(name)
	if err != nil {
		return "", err
	}
	if cookie.Value == "" {
		return "", errors.New("empty username")
	}
	return cookie.Value, nil
}

func newCookie(name, value, path string) *http.Cookie {
	cookie := new(http.Cookie)
	cookie.Name = name
	cookie.Value = value
	cookie.SameSite = http.SameSiteLaxMode
	cookie.Path = path
	cookie.HttpOnly = true
	return cookie
}
