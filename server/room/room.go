package room

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"pixel-collab/server/sse"
	"sync"

	"github.com/labstack/echo/v4"
)

const (
	PATH_ROOM_ID = "roomId"
	COLOR_NONE   = "-"

	EVENT_GRID  = "grid"
	EVENT_USERS = "users"
)

var roomStore = make(map[string]*Room)

type user struct {
	username string
	updates  chan *sse.Event
}

type Room struct {
	ID              string
	PendingActionBy int
	Users           []user
	GridSize        int
	Pixels          [][]string
	mu              sync.Mutex
}

type roomTemplateData struct {
	ID string
}

type pixelUpdate struct {
	Color string `json:"color"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}

func Get(c echo.Context) error {
	_, err := getCookieUsername(c)
	if err != nil {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	roomId := c.Param(PATH_ROOM_ID)
	room := roomStore[roomId]

	if room == nil {
		return errRoomNotFound(c)
	} else {
		c.Render(http.StatusOK, "room.html", roomTemplateData{room.ID})
	}

	return nil
}

func GetEvents(c echo.Context) error {
	username, err := getCookieUsername(c)
	if err != nil {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

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

	// TODO: error handling
	// room updates, initial data
	user := newUser(username)
	room.addUser(user)

	usersEvent, err := room.newUpdateEvent(EVENT_USERS, marshalUsersTo)
	if err == nil {
		room.sendUpdate(usersEvent)
	}

	gridEvent, err := room.newUpdateEvent(EVENT_GRID, marshalGridDataTo)
	if err == nil {
		gridEvent.MarshalTo(w)
	}

	w.Flush()

	// udpates
	for {
		select {
		case <-c.Request().Context().Done():
			log.Printf("SSE client connected, ip: %v", c.RealIP())
			room.removeUser(username)
			return nil

		case update := <-user.updates:
			if err := update.MarshalTo(w); err != nil {
				return err
			}
			w.Flush()
		}
	}
}

func PatchPixel(c echo.Context) error {
	username, err := getCookieUsername(c)
	if err != nil {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	var pixelUpdate pixelUpdate

	if err := c.Bind(&pixelUpdate); err != nil {
		return c.String(http.StatusBadRequest, "Bad request")
	}

	roomId := c.Param(PATH_ROOM_ID)
	room := roomStore[roomId]

	if room == nil {
		return errRoomNotFound(c)
	}

	err = room.updatePixelColor(pixelUpdate.X, pixelUpdate.Y, pixelUpdate.Color, username)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	room.nextTurn()

	gridUpdate, err := room.newUpdateEvent(EVENT_GRID, marshalGridDataTo)
	if err == nil {
		room.sendUpdate(gridUpdate)
	}

	usersUpdate, err := room.newUpdateEvent(EVENT_USERS, marshalUsersTo)
	if err == nil {
		room.sendUpdate(usersUpdate)
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

	roomStore[roomId] = newRoom(roomId, 10)
}

func newUser(username string) user {
	return user{
		username: username,
		updates:  make(chan *sse.Event),
	}
}

func newRoom(id string, gridSize int) *Room {
	pixels := make([][]string, gridSize)
	for i := 0; i < gridSize; i++ {
		pixels[i] = make([]string, gridSize)
		for j := 0; j < gridSize; j++ {
			pixels[i][j] = COLOR_NONE
		}
	}

	return &Room{
		ID:              id,
		Users:           []user{},
		Pixels:          pixels,
		GridSize:        gridSize,
		PendingActionBy: 0,
		mu:              sync.Mutex{},
	}
}

// business logic
func (r *Room) updatePixelColor(x, y int, color, username string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	isUserTurn := r.Users[r.PendingActionBy].username == username
	if !isUserTurn {
		return errors.New("it's not your turn")
	}

	if x < 0 || x >= r.GridSize || y < 0 || y >= r.GridSize {
		return errors.New("invalid pixel coordinates")
	}

	currentColor := r.Pixels[y][x]
	if currentColor != COLOR_NONE {
		return errors.New("pixel color is already set")
	}

	r.Pixels[y][x] = color
	return nil
}

func (r *Room) nextTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.PendingActionBy = max((r.PendingActionBy+1)%len(r.Users), 0)
}

func (r *Room) addUser(user user) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.Users = append(r.Users, user)
}

func (r *Room) removeUser(username string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, user := range r.Users {
		if user.username == username {
			r.Users = append(r.Users[:i], r.Users[i+1:]...)

			// preserve user turn order
			if i <= r.PendingActionBy {
				r.PendingActionBy = max(r.PendingActionBy-1, 0)
			}

			close(user.updates)

			break
		}
	}

	return len(r.Users)
}

// utils
func (r *Room) sendUpdate(update *sse.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, user := range r.Users {
		go func() {
			user.updates <- update
		}()
	}
}

func errRoomNotFound(c echo.Context) error {
	return c.String(http.StatusNotFound, "Room doesn't exist")
}

// marshaling
type udpateMarshalFn func(r *Room, w io.Writer) error

func (r *Room) newUpdateEvent(event string, marshalData udpateMarshalFn) (*sse.Event, error) {
	buffer := bytes.Buffer{}
	err := marshalData(r, &buffer)
	if err != nil {
		return nil, err
	}

	return &sse.Event{
		Data:  buffer.Bytes(),
		Event: []byte(event),
	}, nil
}

func marshalGridDataTo(r *Room, w io.Writer) error {
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

func marshalUsersTo(r *Room, w io.Writer) error {
	pendingActionByUser := ""
	if len(r.Users) > 0 {
		pendingActionByIndex := r.PendingActionBy % len(r.Users)
		pendingActionByUser = r.Users[pendingActionByIndex].username
	}

	if _, err := fmt.Fprint(w, pendingActionByUser); err != nil {
		return err
	}

	for _, user := range r.Users {
		if _, err := fmt.Fprintf(w, ",%s", user.username); err != nil {
			return err
		}
	}

	return nil
}
