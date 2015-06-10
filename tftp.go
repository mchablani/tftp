package main

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"tftputil"
	"time"
)

////////////////////////////////////

func handleReadRequest(req tftputil.RequestPacket, dataConnSend, dataConnRecv *net.UDPConn) error {

	var err error
	var dataConnClientAddr *net.UDPAddr // address of client who sends first data block

	// lookup file exists
	fileAccess.RLock()
	fileData, found := fileAccess.fileMap[string(req.Filename)]
	fileAccess.RUnlock()

	if !found {
		// send error packet and terminate request handling
		errorBuff := tftputil.GetErrorBuffer(tftputil.ErrorPacket{tftputil.ERROR_OPCODE, tftputil.ERROR_TFTP_FILE_NOT_FOUND, []byte("File not found")})
		dataConnSend.Write(errorBuff)

		return nil
	}

	// send file data
	var blockIdAcked uint16 = 0
	dataWriteComplete := false

	const MAX_RETRY_COUNT = 10
	var retries = 0

	for !dataWriteComplete && (retries < MAX_RETRY_COUNT) {
		retries++

		// generate data packet
		var blockId uint16 = blockIdAcked + 1
		var startOffset int = int(blockIdAcked) * tftputil.MAX_DATA_SIZE
		endOffset := startOffset + tftputil.MAX_DATA_SIZE
		if endOffset > len(fileData) {
			endOffset = len(fileData)
		}
		var data []byte = fileData[startOffset:endOffset]
		dataBuff := tftputil.GetDataBuffer(tftputil.DataPacket{tftputil.DATA_OPCODE, blockId, data})

		_, err = dataConnSend.Write(dataBuff)
		if err != nil {
			fmt.Println("error sending block, Error: ", blockId, err)
			continue
		} else {
			//fmt.Println("Data sent blockId", blockId)
		}

		buf := make([]byte, 1024)
		dataConnRecv.SetReadDeadline(time.Now().Add(time.Second * 5))
		rlen, clientAddr, err := dataConnRecv.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Error getting server ack for data sent, err: ", err)
			continue
		}

		if blockIdAcked == 0 {
			// if first data block ack has not been received remember the client addr until its successfully received
			dataConnClientAddr = clientAddr
		} else {
			// if first data block ack has been received expect all from same client
			if clientAddr.String() != dataConnClientAddr.String() {
				fmt.Println("data receive from client, expected client", clientAddr, dataConnClientAddr)
				continue
			}
		}

		var ack tftputil.AckPacket
		ack, err = tftputil.GetAckPacket(buf[0:rlen])
		if err != nil {
			fmt.Println("Error getting client ack for data sent, err: ", err)
			continue
		}

		if ack.BlockId != blockId {
			fmt.Println("ack receive for blockId, expected blockId ", ack.BlockId, blockId)
			continue
		}

		blockIdAcked = blockId
		//fmt.Println("received ack: blockId, serverAddr: ", blockIdAcked, clientAddr)

		// reset retry count
		retries = 0

		// check for completion
		if (int(blockIdAcked) * tftputil.MAX_DATA_SIZE) > len(fileData) {
			dataWriteComplete = true
		}
	}

	if dataWriteComplete {
		return nil
	} else {
		return errors.New("request terminated")
	}
}

func handleWriteRequest(req tftputil.RequestPacket, responseConn, dataConnRecv *net.UDPConn) error {

	var err error
	var dataConnClientAddr *net.UDPAddr // address of client who sends first data block

	// check if file already exists
	fileAccess.RLock()
	_, found := fileAccess.fileMap[string(req.Filename)]
	fileAccess.RUnlock()
	if found {
		// send error packet and terminate request handling
		errorBuff := tftputil.GetErrorBuffer(tftputil.ErrorPacket{tftputil.ERROR_OPCODE, tftputil.ERROR_TFTP_FILE_ALREADY_EXIST, []byte("File Already Exist")})
		responseConn.Write(errorBuff)
		return nil
	}

	// send write ack and wait for data transfer complete
	var blockId uint16 = 0
	fileData := make([]byte, 0, 1024)
	dataRecvComplete := false
	fileCommitError := false
	const MAX_RETRY_COUNT = 10
	var retries = 0

	for retries < MAX_RETRY_COUNT {

		retries++

		// if commit of file failed, dont send ack
		// send error packet and terminate request handling
		if fileCommitError {
			errorBuff := tftputil.GetErrorBuffer(tftputil.ErrorPacket{tftputil.ERROR_OPCODE, tftputil.ERROR_TFTP_FILE_ALREADY_EXIST, []byte("File Already Exist")})
			responseConn.Write(errorBuff)
			return nil
		}

		// send ack for last successful data packet
		ackBuff := tftputil.GetAckBuffer(tftputil.AckPacket{tftputil.ACK_OPCODE, blockId})

		_, err = responseConn.Write(ackBuff)
		if err != nil {
			fmt.Println("error connecting client.  Error: ", err)
			continue
		}

		//fmt.Println("Acq sent, server response addr ", ackBuff, responseConn.LocalAddr())

		if dataRecvComplete {
			// file receive complete and ack sent for last block.  We are done with receive
			break
		}

		// receive next data packet
		buf := make([]byte, 1024)
		dataConnRecv.SetReadDeadline(time.Now().Add(time.Second * 5))
		rlen, addr, err := dataConnRecv.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Error getting client data for block, err: ", blockId+1, err)
			continue
		}

		if blockId == 0 {
			// if first data block has not been received remember the client addr until its successfully received
			dataConnClientAddr = addr
		} else {
			// if first data block has been received expect all from same client
			if addr.String() != dataConnClientAddr.String() {
				fmt.Println("data receive from client, expected client", addr, dataConnClientAddr)
				continue
			}
		}

		var data tftputil.DataPacket
		data, err = tftputil.GetDataPacket(buf[0:rlen])
		if err != nil {
			fmt.Println("Error getting client data for block, err: ", blockId+1, err)
			continue
		}

		if data.BlockId != blockId+1 {
			fmt.Println("data receive for blockId, expected blockId", data.BlockId, blockId+1)
			continue
		}

		// All checks satisfied.  new data is received
		fileData = append(fileData, data.Data[:]...)
		// update block id for ack
		blockId = data.BlockId

		// reset retry count
		retries = 0
		//fmt.Println("name, block, total size received", string(req.filename), blockId, len(fileData))

		if len(data.Data) != tftputil.MAX_DATA_SIZE {
			// end of file
			dataRecvComplete = true

			// commit the file to memory
			fileAccess.Lock()
			// check if file already exist
			_, found := fileAccess.fileMap[string(req.Filename)]
			if !found {
				fileAccess.fileMap[string(req.Filename)] = fileData
			} else {
				fileCommitError = true
			}
			fileAccess.Unlock()

			fmt.Println("file commited: name, size:", string(req.Filename), len(fileData))
		}
	}

	if dataRecvComplete {
		return nil
	} else {
		return errors.New("request terminated")
	}
}

func handleRequest(req tftputil.RequestPacket, clientAddr *net.UDPAddr) error {

	var responseConn, dataConnRecv *net.UDPConn
	var err error

	// connect to client
	responseConn, err = net.DialUDP("udp", nil, clientAddr)
	if err != nil {
		fmt.Println("error connecting to client.  Error: ", err)
		return err
	}
	defer responseConn.Close()

	var addr = responseConn.LocalAddr()
	dataConnServerAddr, _ := net.ResolveUDPAddr("udp", addr.String())

	dataConnRecv, err = net.ListenUDP("udp", dataConnServerAddr)
	if err != nil {
		fmt.Println("error setting up receive channel.  Error: ", err)
		return err
	}
	defer dataConnRecv.Close()

	if req.Opcode == tftputil.RRQ_OPCODE {
		handleReadRequest(req, responseConn, dataConnRecv)
	} else {
		handleWriteRequest(req, responseConn, dataConnRecv)
	}

	return nil
}

func tftpServer() {

	// start TFTP server, bind to SERVER_PORT
	addr := net.UDPAddr{
		Port: tftputil.SERVER_PORT,
		IP:   net.ParseIP("127.0.0.1"),
	}

	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		fmt.Println("TFTP server error listening on udp port.  Error: ", err)
		return
	}
	defer conn.Close()

	fmt.Println("TFTP server start. Port: ", tftputil.SERVER_PORT)

	// read request, validate request, start a thread to handle the request
	buf := make([]byte, 1024)
	for {
		rlen, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Error getting client req: ", err)
			continue
		}

		var req tftputil.RequestPacket
		req, err = tftputil.GetRequestPacket(buf[0:rlen])
		if err != nil {
			fmt.Println("error receiving request packet.  Error, client: ", err, clientAddr)
			continue
		}

		go handleRequest(req, clientAddr)
	}
}

/////////////////////////////
// Global variables
type FILEDATA []byte

var fileAccess = struct {
	sync.RWMutex
	fileMap map[string]FILEDATA
}{fileMap: make(map[string]FILEDATA)}

//var fileMap map[string]FILEDATA

/////////////////////////////

func main() {
	tftpServer()
}
