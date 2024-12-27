package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"first/infr"
)

type User struct {
	ID       string
	Conn     *websocket.Conn
	Metatags []string
}

var (
	usersFindingGame = map[string]*User{}
	mu               sync.Mutex
)

type Container struct {
	Logger *zap.Logger
	Db     *pg.DB
}

func getContainer() *Container {
	logger := infr.NewLogger(true)
	db, err := infr.NewPostgresDatabase("postgresql://user-owner:123123@185.84.163.166:5432/user-db", "cli", logger)
	if err != nil {
		panic("can't work with DB!")
	}

	return &Container{
		Logger: logger,
		Db:     db,
	}
}

type Topic struct {
	ID        int64
	Name      string
	Status    string
	CreatedAt string
}

type Metatopic struct {
	ID        int64
	Name      string
	Status    string
	CreatedAt string
}

type Game struct {
	ID             int64     `pg:"id, pk"`
	FirstPlayerID  int64     `pg:"first_player_id"`
	SecondPlayerID int64     `pg:"second_player_id"`
	WinnerID       int64     `pg:"winner_id"`
	RoomID         string    `pg:"room_uid"`
	MetatopicID    int64     `pg:"metatopic_id"`
	TopicID        int64     `pg:"topic_id"`
	CreatedAt      time.Time `pg:"created_at"`
}

func insertGame(ctx context.Context, db *pg.DB, game Game) error {
	if _, err := db.ModelContext(ctx, &game).Insert(); err != nil {
		return err
	}

	return nil
}

func getTopicByMetatopicName(ctx context.Context, db *pg.DB, metatopicName string) (*Topic, error) {
	var topic Topic

	err := db.ModelContext(ctx, &topic).
		Join("JOIN metatopics_topics mt ON mt.topics_id = topic.id").
		Join("JOIN metatopics m ON m.id = mt.metatopics_id").
		Where(`m.name = ? and topic.status = 'APPROVED'`, metatopicName).Limit(1).
		Select()
	if err != nil {
		return nil, err
	}

	return &topic, nil
}

func getMetatopicByName(ctx context.Context, db *pg.DB, metatopicName string) (*Metatopic, error) {
	var metatopic Metatopic

	err := db.ModelContext(ctx, &metatopic).
		Where(`name = ? and metatopic.status = 'APPROVED'`, metatopicName).Limit(1).
		Select()
	if err != nil {
		return nil, err
	}

	return &metatopic, nil
}

func main() {

	http.HandleFunc("/ws", handleWebSocket)

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Ошибка при запуске сервера:", err)
		return
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Позволяем всем соединениям
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Ошибка при установке WebSocket соединения:", err)
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Ошибка при чтении сообщения:", err)
			break
		}

		var input struct {
			ID       string   `json:"id"`
			Metatags []string `json:"metatags"`
		}

		err = json.Unmarshal(msg, &input)
		if err != nil {
			fmt.Println("Ошибка при разборе JSON:", err)
			continue
		}

		user := &User{ID: input.ID, Conn: conn, Metatags: input.Metatags}

		mu.Lock()
		usersFindingGame[input.ID] = user
		mu.Unlock()

		fmt.Printf("Пользователь %s хочет найти игру\n", input.ID)

		// Поиск пары
		if len(usersFindingGame) > 1 {
			findMatchingPair(user)
		}
	}
}

func findMatchingPair(user *User) {
	cnt := getContainer()

	var bestMatch *User
	var bestMatchScore int
	var fallbackMatch *User // Переменная для хранения запасного соперника

	mu.Lock()
	defer mu.Unlock()

	for _, potentialMatch := range usersFindingGame {
		if potentialMatch.ID == user.ID {
			continue // Пропускаем самого себя
		}

		// Оценка совпадения метатем
		matchScore := calculateMatchScore(user.Metatags, potentialMatch.Metatags)

		if matchScore > bestMatchScore {
			bestMatchScore = matchScore
			bestMatch = potentialMatch
		}

		// Если нет совпадений, сохраняем запасного соперника
		if bestMatchScore == 0 && fallbackMatch == nil {
			fallbackMatch = potentialMatch
		}
	}

	var (
		topic *Topic
		err   error
	)

	topic, err = getTopicByMetatopicName(context.Background(), cnt.Db, user.Metatags[0])
	if err != nil {
		cnt.Logger.Error("can't get topic by metatopic", zap.String("metatags", user.Metatags[0]), zap.Error(err))
	}

	if bestMatch != nil {
		common := lo.Intersect(user.Metatags, bestMatch.Metatags)
		if len(common) > 0 {
			topicMatched, err := getTopicByMetatopicName(context.Background(), cnt.Db, common[0])
			if err != nil {
				cnt.Logger.Error("can't get topic by metatopic", zap.String("metatags", common[0]), zap.Error(err))
			}

			if topicMatched != nil {
				topic = topicMatched
			}
		}

		delete(usersFindingGame, user.ID)
		delete(usersFindingGame, bestMatch.ID)

		fmt.Printf("Пара найдена для пользователей %s и %s\n", user.ID, bestMatch.ID)
		room := uuid.New()

		metatopic, _ := getMetatopicByName(context.Background(), cnt.Db, user.Metatags[0])

		firstUserID, _ := strconv.ParseInt(user.ID, 10, 64)
		secondUserID, _ := strconv.ParseInt(bestMatch.ID, 10, 64)

		game := Game{
			FirstPlayerID:  firstUserID,
			SecondPlayerID: secondUserID,
			RoomID:         room.String(),
			MetatopicID:    metatopic.ID,
			TopicID:        topic.ID,
			CreatedAt:      time.Now(),
		}

		err := insertGame(context.Background(), cnt.Db, game)
		if err != nil {
			fmt.Println(err)
		}

		sendResponse(user, bestMatch, room.String(), topic.Name)
	} else {
		delete(usersFindingGame, user.ID)
		delete(usersFindingGame, fallbackMatch.ID)

		room := uuid.New()
		metatopic, _ := getMetatopicByName(context.Background(), cnt.Db, user.Metatags[0])

		firstUserID, _ := strconv.ParseInt(user.ID, 10, 64)
		secondUserID, _ := strconv.ParseInt(fallbackMatch.ID, 10, 64)

		fmt.Printf("Нет подходящей пары по метатемам для пользователя %s, будет взят запасной соперник %s\n", user.ID, fallbackMatch.ID)
		game := Game{
			FirstPlayerID:  firstUserID,
			SecondPlayerID: secondUserID,
			RoomID:         room.String(),
			MetatopicID:    metatopic.ID,
			TopicID:        topic.ID,
			CreatedAt:      time.Now(),
		}

		err := insertGame(context.Background(), cnt.Db, game)
		if err != nil {
			fmt.Println(err)
		}

		sendResponse(user, fallbackMatch, room.String(), topic.Name)
	}
}

// Функция для оценки совпадения метатем
func calculateMatchScore(metatags1, metatags2 []string) int {
	score := 0

	// Сравниваем подстроки
	for _, tag1 := range metatags1 {
		for _, tag2 := range metatags2 {
			if tag1 == tag2 {
				score++
			}
		}
	}
	return score
}

func sendWithRetry(conn *websocket.Conn, message string, id string) {
	maxWait := 10 * time.Second
	waitTime := time.Second

	for {
		err := conn.WriteMessage(websocket.TextMessage, []byte(message))
		if err == nil {
			return
		}
		fmt.Println("Ошибка при отправке ответа:", id, err)
		time.Sleep(waitTime)

		if waitTime < maxWait {
			waitTime *= 2
			if waitTime > maxWait {
				return
			}
		}
	}
}

func sendResponse(user1, user2 *User, room string, theme string) {
	messagefirst := fmt.Sprintf(`{"room": "%s", "startUserId": "%s", "opponent": "%s", "theme":"%s"}`, room, user1.ID, user2.ID, theme)
	messageSecond := fmt.Sprintf(`{"room": "%s", "startUserId": "%s", "opponent": "%s", "theme":"%s"}`, room, user1.ID, user1.ID, theme)
	sendWithRetry(user1.Conn, messagefirst, user1.ID)
	sendWithRetry(user2.Conn, messageSecond, user2.ID)
}
