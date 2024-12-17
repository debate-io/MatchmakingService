package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type User struct {
	ID       string
	Conn     *websocket.Conn
	Metatags []string
}

var usersFindingGame = map[string]*User{}
var sendChan chan string
var mu sync.Mutex

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

		mu.Lock()
		usersFindingGame[input.ID] = user
		mu.Unlock()

		sendChan <- input.ID

		fmt.Printf("Пользователь %s хочет найти игру\n", input.ID)

		// Поиск пары
		findMatchingPair(user)
	}
}

func findMatchingPair(user *User) {
	mu.Lock()
	defer mu.Unlock()

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

	if bestMatch != nil {
		delete(usersFindingGame, user.ID)
		delete(usersFindingGame, bestMatch.ID)

		fmt.Printf("Пара найдена для пользователей %s и %s\n", user.ID, bestMatch.ID)
		sendResponse(user, bestMatch)
	} else {
		delete(usersFindingGame, user.ID)
		delete(usersFindingGame, fallbackMatch.ID)

		fmt.Printf("Нет подходящей пары по метатемам для пользователя %s, будет взят запасной соперник %s\n", user.ID, fallbackMatch.ID)
		sendResponse(user, fallbackMatch)
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
				waitTime = maxWait
			}
		}
	}
}

func sendResponse(user1, user2 *User) {
	room := uuid.New()
	message := fmt.Sprintf(`{"room": "%s", "startUserId": "%s"}`, room, user1.ID)
	sendWithRetry(user1.Conn, message, user1.ID)
	sendWithRetry(user2.Conn, message, user2.ID)
}
