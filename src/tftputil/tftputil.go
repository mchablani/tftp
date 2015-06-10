package tftputil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

/////////////////////////////
// TFTP consts and packet formats

const RRQ_OPCODE uint16 = 1
const WRQ_OPCODE uint16 = 2
const DATA_OPCODE uint16 = 3
const ACK_OPCODE uint16 = 4
const ERROR_OPCODE uint16 = 5

const MAX_DATA_SIZE = 512

var OCTET_MODE = []byte("octet")
var NETASCII_MODE = []byte("netascii")

const SERVER_PORT int = 69

const ERROR_TFTP_FILE_NOT_FOUND uint16 = 1
const ERROR_TFTP_FILE_ALREADY_EXIST uint16 = 6

/////////////////////////////

type RequestPacket struct {
	Opcode   uint16
	Filename []byte
	Mode     []byte
}

type DataPacket struct {
	Opcode  uint16
	BlockId uint16
	Data    []byte
}

type AckPacket struct {
	Opcode  uint16
	BlockId uint16
}

type ErrorPacket struct {
	Opcode    uint16
	ErrorCode uint16
	ErrorMsg  []byte
}

/////////////////////////////

func GetPacketOpcode(buff []byte) (uint16, error) {

	var opcode uint16
	rdr := bytes.NewReader(buff)

	binary.Read(rdr, binary.BigEndian, &opcode)
	return opcode, nil
}

func GetAckPacket(buff []byte) (AckPacket, error) {

	var packet AckPacket
	rdr := bytes.NewReader(buff)

	binary.Read(rdr, binary.BigEndian, &packet.Opcode)
	if packet.Opcode != ACK_OPCODE {
		fmt.Println("received opcode: ", packet.Opcode)
		return packet, errors.New("Invalid opcode")
	}

	binary.Read(rdr, binary.BigEndian, &packet.BlockId)

	return packet, nil
}

func GetDataPacket(buff []byte) (DataPacket, error) {

	var packet DataPacket
	rdr := bytes.NewReader(buff)

	binary.Read(rdr, binary.BigEndian, &packet.Opcode)
	if packet.Opcode != DATA_OPCODE {
		fmt.Println("received request: opcode: ", packet.Opcode)
		return packet, errors.New("Invalid data opcode")
	}

	binary.Read(rdr, binary.BigEndian, &packet.BlockId)

	packet.Data = buff[4:]

	return packet, nil
}

func GetRequestPacket(buff []byte) (RequestPacket, error) {

	var packet RequestPacket
	rdr := bytes.NewReader(buff)

	binary.Read(rdr, binary.BigEndian, &packet.Opcode)
	if (packet.Opcode != RRQ_OPCODE) && (packet.Opcode != WRQ_OPCODE) {
		fmt.Println("received request: opcode: ", packet.Opcode)
		return packet, errors.New("Invalid request opcode")
	}

	var temp byte
	binary.Read(rdr, binary.BigEndian, &temp)
	for i := 0; temp != byte(0); i++ {
		packet.Filename = append(packet.Filename, temp)
		binary.Read(rdr, binary.BigEndian, &temp)
	}

	binary.Read(rdr, binary.BigEndian, &temp)
	for i := 0; temp != byte(0); i++ {
		packet.Mode = append(packet.Mode, temp)
		binary.Read(rdr, binary.BigEndian, &temp)
	}

	if !bytes.Equal(packet.Mode, OCTET_MODE) && !bytes.Equal(packet.Mode, NETASCII_MODE) {
		fmt.Println("received request: mode:", string(packet.Mode))
		return packet, errors.New("Invalid request mode")
	}

	fmt.Println("received request: ", packet.Opcode, string(packet.Filename), string(packet.Mode))

	return packet, nil
}

/////////////////////////////

func GetErrorBuffer(packet ErrorPacket) []byte {

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, packet.Opcode)
	binary.Write(buf, binary.BigEndian, packet.ErrorCode)
	binary.Write(buf, binary.BigEndian, packet.ErrorMsg)
	binary.Write(buf, binary.BigEndian, byte(0))
	return buf.Bytes()
}

func GetAckBuffer(packet AckPacket) []byte {

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, packet.Opcode)
	binary.Write(buf, binary.BigEndian, packet.BlockId)
	//fmt.Printf("AckBuffer. Len: %d, buff: %s\n", len(buf.Bytes()), buf.Bytes())

	return buf.Bytes()
}

func GetDataBuffer(packet DataPacket) []byte {

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, packet.Opcode)
	binary.Write(buf, binary.BigEndian, packet.BlockId)

	databuff := buf.Bytes()
	databuff = append(databuff, packet.Data[:]...)

	//fmt.Printf("DataBuffer. Len: %d, buff: %s\n", len(databuff), databuff)

	return databuff
}

func GetRequestBuffer(packet RequestPacket) []byte {

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, packet.Opcode)
	binary.Write(buf, binary.BigEndian, packet.Filename)
	binary.Write(buf, binary.BigEndian, byte(0))
	binary.Write(buf, binary.BigEndian, packet.Mode)
	binary.Write(buf, binary.BigEndian, byte(0))
	//fmt.Printf("RequestBuffer. Len: %d, buff: %s\n", len(buf.Bytes()), buf.Bytes())

	return buf.Bytes()
}

/////////////////////////////
