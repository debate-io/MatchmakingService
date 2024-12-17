Этот WebSocket сервер позволяет пользователям подключаться и искать соперников на основе метатегов. Пользователи могут отправлять свои метатеги, и сервер будет пытаться найти подходящего соперника.

Конечная точка
- URL: /ws
- Метод: GET
- Протокол: WebSocket

URL: ws://host:port/ws

Подключение к WebSocket
      Для подключения к WebSocket серверу, вы можете использовать JavaScript в браузере или любой другой язык программирования, поддерживающий WebSocket.
  Вот пример на JavaScript:

      // Создаем новое WebSocket соединение
        const socket = new WebSocket('ws://localhost:8080/ws');
    
        // Обработчик события открытия соединения
        socket.onopen = function(event) {
        console.log('Соединение установлено');
    
        // Отправляем данные о пользователе
        const userData = {
        id: 'user123',
        metatags: ['game', 'fun', 'strategy']
        };
        socket.send(JSON.stringify(userData));
        };

        // Обработчик события получения сообщения
        socket.onmessage = function(event) {
            const response = JSON.parse(event.data);
            if (response.found) {
                console.log(`Найден соперник: ${response.opponent}`);
            }
        };
        
        // Обработчик события ошибки
        socket.onerror = function(error) {
            console.error('Ошибка WebSocket:', error);
        };
        
        // Обработчик события закрытия соединения
        socket.onclose = function(event) {
            console.log('Соединение закрыто', event);
        };

Формат отправляемых данных
      При отправке данных на сервер, используйте следующий формат JSON:

      {
          "id": "user123",
          "metatags": ["tag1", "tag2", "tag3"]
      }

Сервер отправляет ответ в формате JSON, который содержит уникальный uid:

      {
          "room": "1312323",
          "startUserId": "1111"
      }
