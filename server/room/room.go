package room

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"pixel-collab/server/sse"
	"pixel-collab/server/util"
	"sync"

	"github.com/labstack/echo/v4"
)

const (
	PATH_ROOM_ID = "roomId"

	COLOR_NONE = "-"

	EVENT_GRID  = "grid"
	EVENT_USERS = "users"

	GRID_SIZE = 16
)

var (
	storeMutex = sync.Mutex{}
	roomStore  = make(map[string]*room)
)

type user struct {
	username string
	updates  chan *sse.Event
}

type room struct {
	id              string
	pendingActionBy int
	users           []user
	gridSize        int
	pixels          [][]string
	lastGeneratedId int
	mu              sync.Mutex
}

type pixelUpdateRequest struct {
	Color string `json:"color"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}

type roomTemplateData struct {
	ID         string
	Username   string
	InviteLink string
}

// hanlders
func Get(c echo.Context) error {
	requestedUsername, err := getNonEmptyCookie(c, COOKIE_REQUESTED_USERNAME)
	if err != nil {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	roomId := c.Param(PATH_ROOM_ID)
	// TODO: validate roomId
	room := getOrCreateRoom(roomId)

	username := room.generateUniqueUsername(requestedUsername)
	c.SetCookie(newCookie(COOKIE_USERNAME, username, "/room/"+roomId))

	data := roomTemplateData{
		ID:         roomId,
		Username:   username,
		InviteLink: fmt.Sprintf("%s/join?roomId=%s", util.GetBaseUrl(), roomId),
	}
	c.Render(http.StatusOK, "room.html", data)

	return nil
}

func GetEvents(c echo.Context) error {
	username, err := getNonEmptyCookie(c, COOKIE_USERNAME)
	if err != nil {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	roomId := c.Param(PATH_ROOM_ID)
	room := getOrCreateRoom(roomId)

	// sse setup
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// room updates, initial data write
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
			log.Printf("room %s user %s disconnected", roomId, username)

			shouldNotifyUsers := onUserDisconnect(room, username)
			if shouldNotifyUsers {
				usersEvent, err := room.newUpdateEvent(EVENT_USERS, marshalUsersTo)
				if err == nil {
					room.sendUpdate(usersEvent)
				}
			}

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
	username, err := getNonEmptyCookie(c, COOKIE_USERNAME)
	if err != nil {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	var updateRequest pixelUpdateRequest

	if err := c.Bind(&updateRequest); err != nil {
		return c.String(http.StatusBadRequest, "Bad request")
	}

	roomId := c.Param(PATH_ROOM_ID)
	room := getRoom(roomId)

	if room == nil {
		return errRoomNotFound(c)
	}

	err = room.updatePixelColor(updateRequest.X, updateRequest.Y, updateRequest.Color, username)
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

// state management
func getOrCreateRoom(roomId string) *room {
	storeMutex.Lock()
	defer storeMutex.Unlock()

	room := roomStore[roomId]
	if room != nil {
		return room
	}

	room = newRoom(roomId, GRID_SIZE)
	roomStore[roomId] = room
	log.Println("room", roomId, "created")
	return room
}

func onUserDisconnect(room *room, username string) bool {
	storeMutex.Lock()
	defer storeMutex.Unlock()
	usersLeft := room.removeUser(username)
	if usersLeft == 0 {
		delete(roomStore, room.id)
		log.Println("room", room.id, "deleted")
		return false
	}

	return true
}

func getRoom(roomId string) *room {
	storeMutex.Lock()
	defer storeMutex.Unlock()

	return roomStore[roomId]
}

func newUser(username string) user {
	return user{
		username: username,
		updates:  make(chan *sse.Event),
	}
}

func newRoom(id string, gridSize int) *room {
	pixels := make([][]string, gridSize)
	for i := 0; i < gridSize; i++ {
		pixels[i] = make([]string, gridSize)
		for j := 0; j < gridSize; j++ {
			pixels[i][j] = COLOR_NONE
		}
	}

	return &room{
		id:              id,
		users:           []user{},
		pixels:          pixels,
		gridSize:        gridSize,
		pendingActionBy: 0,
		mu:              sync.Mutex{},
		lastGeneratedId: -1,
	}
}

// business logic
func (r *room) updatePixelColor(x, y int, color, username string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	isUserTurn := r.users[r.pendingActionBy].username == username
	if !isUserTurn {
		return errors.New("it's not your turn")
	}

	if x < 0 || x >= r.gridSize || y < 0 || y >= r.gridSize {
		return errors.New("invalid pixel coordinates")
	}

	currentColor := r.pixels[y][x]
	if currentColor != COLOR_NONE {
		return errors.New("pixel color is already set")
	}

	r.pixels[y][x] = color
	return nil
}

func (r *room) nextTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pendingActionBy = max((r.pendingActionBy+1)%len(r.users), 0)
}

func (r *room) addUser(user user) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.users = append(r.users, user)
}

func (r *room) removeUser(username string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, user := range r.users {
		if user.username == username {
			r.users = append(r.users[:i], r.users[i+1:]...)

			// preserve user turn order
			if i <= r.pendingActionBy {
				r.pendingActionBy = max(r.pendingActionBy-1, 0)
			}

			close(user.updates)

			break
		}
	}

	return len(r.users)
}

func (r *room) generateUniqueUsername(base string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastGeneratedId++
	return fmt.Sprintf("%s#%04d", base, r.lastGeneratedId)
}

// utils
func (r *room) sendUpdate(update *sse.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, user := range r.users {
		go func() {
			user.updates <- update
		}()
	}
}

func errRoomNotFound(c echo.Context) error {
	return c.String(http.StatusNotFound, "Room doesn't exist")
}

// marshaling
type udpateMarshalFn func(r *room, w io.Writer) error

func (r *room) newUpdateEvent(event string, marshalData udpateMarshalFn) (*sse.Event, error) {
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

func marshalGridDataTo(r *room, w io.Writer) error {
	if _, err := fmt.Fprint(w, r.gridSize); err != nil {
		return err
	}

	for i := 0; i < r.gridSize; i++ {
		for j := 0; j < r.gridSize; j++ {
			if _, err := fmt.Fprintf(w, ",%s", r.pixels[i][j]); err != nil {
				return err
			}
		}
	}

	return nil
}

func marshalUsersTo(r *room, w io.Writer) error {
	pendingActionByUser := ""
	if len(r.users) > 0 {
		pendingActionByIndex := r.pendingActionBy % len(r.users)
		pendingActionByUser = r.users[pendingActionByIndex].username
	}

	if _, err := fmt.Fprint(w, pendingActionByUser); err != nil {
		return err
	}

	for _, user := range r.users {
		if _, err := fmt.Fprintf(w, ",%s", user.username); err != nil {
			return err
		}
	}

	return nil
}
