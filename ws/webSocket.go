package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
)

func AcceptHandshake(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.ReadWriter, error) {

	// Проверяем заголовки, смотрим, готов ли клиент перейти на другой протокол
	if r.Header.Get("Upgrade") != "websocket" {
		return nil, nil, errors.New("Upgrade не равен websocket")
	}
	if r.Header.Get("Connection") != "Upgrade" {
		return nil, nil, errors.New("Connection не равен Upgrade")
	}

	// Получаем ключ
	k := r.Header.Get("Sec-Websocket-Key")
	if k == "" {
		return nil, nil, errors.New("Sec-Websocket-Key равен нулю")
	}

	// Вычисляем ключ для подтверждения соединения
	sum := k + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	hash := sha1.Sum([]byte(sum))
	str := base64.StdEncoding.EncodeToString(hash[:])

	// Берем под контроль соединение
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("Не получилось взять под контроль соединение")
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}

	// Формируем и отправляем ответ клиенту
	bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	bufrw.WriteString("Upgrade: websocket\r\n")
	bufrw.WriteString("Connection: Upgrade\r\n")
	bufrw.WriteString("Sec-Websocket-Accept: " + str + "\r\n\r\n")
	bufrw.Flush()

	return conn, bufrw, nil
}

func Write(buf *bufio.ReadWriter, msg string) error {
	msgByte := []byte(msg)

	// Создаем заголовок
	frame := make([]byte, 2)

	// Задаем последние 4 бита, указывающие на код операции
	frame[0] |= 1 // 0000 0001 (текстовые данные)

	// Задаем первый бит, указывающий на финальный фрейм
	frame[0] |= 128 // 1000 0000

	length := len(msgByte)

	switch {
	case length < 126:

		// Записываем в фрейм объем
		frame[1] |= byte(length) 
	case length == 126:

		// Обозначаем, что дальше будет еще 2 бита для обозначения длины полезной нагрузки
		frame[1] |= 126 // 1111 1110

		// Создаем два байта
		size := make([]byte, 2)

		// Записываем в них объем
		binary.BigEndian.PutUint16(size, uint16(length))

		// Записываем в фрейм
		frame = append(frame, size...)

	case length > 126:

		// Обозначаем, что дальше будет еще 8 битов для обозначения длины полезной нагрузки
		frame[1] |= 127 // 1111 1111

		// Создаем 8 байт
		size := make([]byte, 8)

		// Записываем в них объем
		binary.BigEndian.PutUint64(size, uint64(length))

		// Записываем в фрейм
		frame = append(frame, size...)
	}
	
	// Наконец записываем в буфер наше сообщение
	frame = append(frame, msgByte...)
	
	// Пишем в буфер
	if _, err := buf.Write(frame); err != nil {
		return err
	}

	// Отправляем
	if err := buf.Flush(); err != nil {
		return err
	}

	return nil
}
