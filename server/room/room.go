package room

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"pixel-collab/server/sse"

	"github.com/labstack/echo/v4"
)

const (
	PATH_ROOM_ID = "roomId"
	COLOR_NONE   = "-"
)

var roomStore = make(map[string]*Room)

type User struct {
	ID   string
	Name string
}

type Room struct {
	ID       string
	Users    []User
	GridSize int
	Pixels   [][]string
	Updates  chan *sse.Event
}

type pixelUpdate struct {
	Color string `json:"color"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}

func Get(c echo.Context) error {
	username, err := getCookieUsername(c)
	if err != nil {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	log.Println(username)

	roomId := c.Param(PATH_ROOM_ID)
	room := roomStore[roomId]

	if room == nil {
		return errRoomNotFound(c)
	} else {
		c.Render(http.StatusOK, "room.html", *room)
	}

	return nil
}

func GetEvents(c echo.Context) error {
	roomId := c.Param(PATH_ROOM_ID)
	room := roomStore[roomId]

	if room == nil {
		return errRoomNotFound(c)
	}

	// sse setup
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// initial data
	event, err := room.gridUpdateEvent()
	if err == nil {
		event.MarshalTo(w)
		w.Flush()
	}

	// udpates
	for {
		select {
		case <-c.Request().Context().Done():
			log.Printf("SSE client connected, ip: %v", c.RealIP())
			return nil

		case update := <-room.Updates:
			if err := update.MarshalTo(w); err != nil {
				return err
			}
			w.Flush()
		}
	}
}

func PatchPixel(c echo.Context) error {
	var pixelUpdate pixelUpdate

	if err := c.Bind(&pixelUpdate); err != nil {
		return c.String(http.StatusBadRequest, "Bad request")
	}

	roomId := c.Param(PATH_ROOM_ID)
	room := roomStore[roomId]

	if room == nil {
		return errRoomNotFound(c)
	}

	currentColor := room.Pixels[pixelUpdate.Y][pixelUpdate.X]
	if currentColor != COLOR_NONE {
		return c.String(http.StatusConflict, "Pixel color is already set")
	}

	room.Pixels[pixelUpdate.Y][pixelUpdate.X] = pixelUpdate.Color
	event, err := room.gridUpdateEvent()
	if err == nil {
		go func() {
			room.Updates <- event
		}()
	}

	return c.NoContent(http.StatusOK)
}

func InitTestRoom(roomId string) {
	pixels := make([][]string, 10)
	for i := 0; i < 10; i++ {
		pixels[i] = make([]string, 10)
		for j := 0; j < 10; j++ {
			pixels[i][j] = COLOR_NONE
		}
	}

	roomStore[roomId] = &Room{
		ID:       roomId,
		Users:    []User{},
		Updates:  make(chan *sse.Event),
		Pixels:   pixels,
		GridSize: 10,
	}
}

func errRoomNotFound(c echo.Context) error {
	return c.String(http.StatusNotFound, "Room doesn't exist")
}

// marshaling
func (r *Room) gridUpdateEvent() (*sse.Event, error) {
	buffer := bytes.Buffer{}
	err := r.marshalGridDataTo(&buffer)
	if err != nil {
		return nil, err
	}

	return &sse.Event{
		Data:  buffer.Bytes(),
		Event: []byte("grid"),
	}, nil
}

func (r *Room) marshalGridDataTo(w io.Writer) error {
	if _, err := fmt.Fprint(w, r.GridSize); err != nil {
		return err
	}

	for i := 0; i < r.GridSize; i++ {
		for j := 0; j < r.GridSize; j++ {
			if _, err := fmt.Fprintf(w, ",%s", r.Pixels[i][j]); err != nil {
				return err
			}
		}
	}

	return nil
}
