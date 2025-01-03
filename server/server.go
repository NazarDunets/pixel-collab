package main

import (
	"pixel-collab/server/room"
	"pixel-collab/server/util"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

const TEST_ID = "1234"

func main() {
	e := echo.New()

	util.SetupTemplates(e)
	room.InitTestRoom("1234")

	e.Use(middleware.Logger())

	e.File("/", "static/auth.html")
	e.POST("/join", room.PostJoin)

	e.GET("/room/:"+room.PATH_ROOM_ID, room.Get)
	e.GET("/room/:"+room.PATH_ROOM_ID+"/events", room.GetEvents)
	e.PATCH("/room/:"+room.PATH_ROOM_ID+"/pixel", room.PatchPixel)

	e.Logger.Fatal(e.Start("localhost:1323"))
}