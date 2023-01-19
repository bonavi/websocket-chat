package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"webSocket/ws"
)

// Лимит подключений в хабе
const hubLimit = 2

// Обозначаем структуру кастомного сервера
type WsServer struct {
	// Слайс клиентов
	Clients  []*Client
	// Канал для передачи клиента в сервер
	register chan *Client
}

func NewWsServer() *WsServer {
	return &WsServer{
		Clients:  make([]*Client, 0),
		register: make(chan *Client),
	}
}

// Функция, которая будет запускаться при старте сервера и слушать stdin для отправки сообщений
// и канал, для добавления пользователей в массив
func (ws *WsServer) Run() {

	go func() {
		for {

			// Сканируем Stdin
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			msg := scanner.Text()

			// Разделяем строку по пробелам
			msgArr := strings.Split(msg, " ")

			// Обозначаем целевых клиентов
			var targetClients []*Client

			if len(msgArr) < 3 {
				fmt.Println("Неверный формат сообщения")
				return
			}

			switch {
			case msgArr[0] == "send" && msgArr[1] == "--hub":

				// Конверируем идентификатор хаба
				hubID, err := strconv.Atoi(msgArr[2])
				if err != nil {
					fmt.Println("Введен неверный идентификатор хаба. Error: %w", err)
					return
				}
				
				// Проходим по массиву клиентов и выбираем пользователей с необходимым хабом
				for _, client := range ws.Clients {
					if hubID == client.HubID {
						targetClients = append(targetClients, client)
					}
				}

				// Восстанавливаем сообщение
				msg = strings.Join(msgArr[3:], " ")

			case msgArr[0] == "sendc" && msgArr[1] == "--id":

				// Конверируем идентификатор клиента
				clientID, err := strconv.Atoi(msgArr[2])
				if err != nil {
					fmt.Println("Введен неверный идентификатор пользователя. Error: %w", err)
					return
				}

				// Проходим по массиву клиентов и выбираем нужного пользователя
				for _, client := range ws.Clients {
					if clientID == client.ID {
						targetClients = append(targetClients, client)
						break
					}
				}

				// Восстанавливаем сообщение
				msg = strings.Join(msgArr[3:], " ")
			default:
				fmt.Println("Неверный формат сообщения")
			}

			if len(targetClients) == 0 {
				fmt.Println("Нет пользователей, которым должно прийти сообщение")
			}

			// Отправляем сообщение в канал выбранным пользователям
			for _, client := range targetClients {
				client.msgChan <- msg
			}
		}
	}()

	// Как только в канал попадает клиент, мы добавляем его в список
	for {
		select {
		case client := <-ws.register:
			ws.Clients = append(ws.Clients, client)
		}
	}
}






func main() {

	// Получаем наш кастомный сервер
	wsServer := NewWsServer()

	// Прослушиваем канал регистрации пользователей и stdin
	go wsServer.Run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wsHandle(wsServer, w, r)
	})

	fmt.Println("Server is listening port :8000")
	log.Fatal(http.ListenAndServe(":8000", nil))
}

// Как только получили запрос на соединение
func wsHandle(wsServer *WsServer, w http.ResponseWriter, r *http.Request) {

	// Пожимаем руки и перехватываем подключение 
	conn, bufrw, err := ws.AcceptHandshake(w, r)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	// Создаем нового клиента
	client := NewClient(wsServer, conn, bufrw)

	// Запускаем горутину, которая ждет на вход в канал сообщение и отправляет его пользователю
	go client.WaitWrite()

	// Регистрируем пользователя
	wsServer.register <- client
	
	// Пишем сообщение с идентификатором клиента и хаба для удобства тестирования
	client.msgChan <- fmt.Sprintf("ClientID: %d, HubID: %d", client.ID, client.HubID)
}








// Структура клиента
type Client struct {
	// Идентификатор клиента
	ID       int
	// Идентификатор хаба, которому принадлежит клиент
	HubID    int
	// Подключение
	conn     net.Conn
	// Буфер, через который мы общаемся с клиентом
	buffer   *bufio.ReadWriter
	// Канал для передачи сообщения
	msgChan  chan string
	// Ссылка на кастомный сервер
	wsServer *WsServer
}

func NewClient(wsServer *WsServer, conn net.Conn, buf *bufio.ReadWriter) *Client {

	hubs := make(map[int]int)
	var selectHub int
	var clientID int

	// Пробегаемся по всем клиентам
	for _, client := range wsServer.Clients {
		// Обозначаем напротив каждого номера хаба количество клиентов в нем
		hubs[client.HubID]++
		// Ищем максимальный идентификатор клиента
		if clientID <= client.ID {
			clientID = client.ID + 1
		}
	}

	// Проходимся по мапе и ищем незаполненный хаб, если нашли, присваиваем его клиенту
	for key := range hubs {
		if hubs[key] < hubLimit {
			selectHub = key
			break
		}
	}

	// Если это первый клиент, делаем ему первый ID
	if clientID == 0 {
		clientID = 1
	}

	// Если незаполненный хаб не нашелся, создаем новый
	if selectHub == 0 {
		selectHub = len(hubs) + 1
	}

	return &Client{
		ID:       clientID,
		HubID:    selectHub,
		conn:     conn,
		buffer:   buf,
		msgChan:  make(chan string, 256),
		wsServer: wsServer,
	}
}

// Функция прослушивания канала входящих сообщений
func (client *Client) WaitWrite() {
	for {
		select {
		case msg := <-client.msgChan:
			if err := ws.Write(client.buffer, msg); err != nil {
				fmt.Println(err.Error())
				return

			}
		}
	}
}
