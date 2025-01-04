package main

import (
	"log"
	"net/http"
	"pixel-collab/server/room"
	"pixel-collab/server/util"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	e := echo.New()

	if err := godotenv.Load(); err != nil {
		log.Fatalln("No .env file found")
	}

	util.SetupTemplates(e)

	e.Use(middleware.Logger())

	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusTemporaryRedirect, "/join")
	})

	e.GET("/join", room.GetJoin)
	e.POST("/join", room.PostJoin)

	e.GET("/room/:"+room.PATH_ROOM_ID, room.Get)
	e.GET("/room/:"+room.PATH_ROOM_ID+"/events", room.GetEvents)
	e.PATCH("/room/:"+room.PATH_ROOM_ID+"/pixel", room.PatchPixel)

	e.Logger.Fatal(e.Start(util.GetStartAddress()))
}
