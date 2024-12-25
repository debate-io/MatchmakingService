package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

var usersFindingGame = map[string]*User{}
var sendChan chan string

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

func main() {
	sendChan = make(chan string, 100)

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
		usersFindingGame[input.ID] = user

		sendChan <- input.ID

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
		sendResponse(user, bestMatch, topic.Name)
	} else {
		delete(usersFindingGame, user.ID)
		delete(usersFindingGame, fallbackMatch.ID)

		fmt.Printf("Нет подходящей пары по метатемам для пользователя %s, будет взят запасной соперник %s\n", user.ID, fallbackMatch.ID)
		sendResponse(user, fallbackMatch, topic.Name)
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

func sendResponse(user1, user2 *User, theme string) {
	room := uuid.New()
	messagefirst := fmt.Sprintf(`{"room": "%s", "startUserId": "%s", "opponent": "%s", "theme":"%s"}`, room, user1.ID, user2.ID, theme)
	messageSecond := fmt.Sprintf(`{"room": "%s", "startUserId": "%s", "opponent": "%s", "theme":"%s"}`, room, user1.ID, user1.ID, theme)
	sendWithRetry(user1.Conn, messagefirst, user1.ID)
	sendWithRetry(user2.Conn, messageSecond, user2.ID)
}
