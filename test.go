package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"tftputil"
	"time"
)

/////////////////////////////

func getRandomByteBuffer(n int) ([]byte, error) {
	buff := make([]byte, n)
	_, err := rand.Read(buff)

	return buff, err
}

func getRandomPort() uint16 {
	buff := make([]byte, 2)
	rand.Read(buff)

	var port uint16 = uint16(buff[0]) + (uint16(buff[1]) << 8)
	return port
}

/////////////////////////////

func sendRequestAndEstablishDataConnection(rqBuff []byte) ( /*dataConnRecv*/ *net.UDPConn /*dataConnSend*/, *net.UDPConn /*dataConnServerAddr*/, *net.UDPAddr, error) {

	// connect to TFTP server

	destAddr := net.UDPAddr{
		Port: tftputil.SERVER_PORT,
		IP:   net.ParseIP("127.0.0.1"),
	}

	var err error
	var reqConn, dataConnRecv, dataConnSend *net.UDPConn
	var dataConnServerAddr *net.UDPAddr

	reqConn, err = net.DialUDP("udp", nil, &destAddr)
	if err != nil {
		fmt.Println("error connecting to TFTP server.  Error: ", err)
		return dataConnRecv, dataConnSend, dataConnServerAddr, err
	}
	defer reqConn.Close()

	var addr = reqConn.LocalAddr()
	dataConnClientAddr, _ := net.ResolveUDPAddr("udp", addr.String())
	//fmt.Println("req conn", reqConn)
	//fmt.Printf("clinet addr %s\n", dataConnClientAddr)

	dataConnRecv, err = net.ListenUDP("udp", dataConnClientAddr)
	if err != nil {
		fmt.Println("error receiving response to rd/wr req.  Error: ", err)
		return dataConnRecv, dataConnSend, dataConnServerAddr, err
	}

	//fmt.Println("data conn recv", dataConnRecv)

	reqAcked := false
	const MAX_RETRY_COUNT = 10
	var retries = 0

	for !reqAcked && (retries < MAX_RETRY_COUNT) {
		retries++

		_, err = reqConn.Write(rqBuff)
		if err != nil {
			fmt.Println("error connecting to TFTP server.  Error: ", err)
			continue
		}
		//fmt.Println("req sent")

		buf := make([]byte, 1024)
		dataConnRecv.SetReadDeadline(time.Now().Add(time.Second * 5))
		var rlen int
		rlen, dataConnServerAddr, err = dataConnRecv.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Error getting server ack for request, err: ", err)
			continue
		}

		opcode, _ := tftputil.GetPacketOpcode(buf[0:rlen])

		// for write request we expect ack
		if opcode == tftputil.ACK_OPCODE {
			var ack tftputil.AckPacket
			ack, err = tftputil.GetAckPacket(buf[0:rlen])
			if err != nil {
				fmt.Println("Error getting server ack for request, err: ", err)
				continue
			}

			if ack.BlockId != uint16(0) {
				fmt.Println("ack receive for blockId, expected blockId 0", ack.BlockId)
				continue
			}

			reqAcked = true
		}

		// for read request data packet is the ack
		if opcode == tftputil.DATA_OPCODE {
			var data tftputil.DataPacket
			data, err = tftputil.GetDataPacket(buf[0:rlen])
			if err != nil {
				fmt.Println("Error getting server ack for request, err: ", err)
				continue
			}

			if data.BlockId != uint16(1) {
				fmt.Println("data block receives for blockId, expected blockId 1", data.BlockId)
				continue
			}

			reqAcked = true
		}

		if opcode == tftputil.ERROR_OPCODE {
			fmt.Println("request rejected")
			return dataConnRecv, dataConnSend, dataConnServerAddr, errors.New("request rejected")
		}

		//fmt.Println("received req ack/data: serverAddr: ", dataConnServerAddr)
	}

	if !reqAcked {
		return dataConnRecv, dataConnSend, dataConnServerAddr, errors.New("request rejected")
	}

	dataConnSend, err = net.DialUDP("udp", nil, dataConnServerAddr)
	if err != nil {
		fmt.Println("error connecting to TFTP server for data send.  Error: ", err)
		return dataConnRecv, dataConnSend, dataConnServerAddr, err
	}
	//fmt.Println("dataConnSend conn", dataConnSend)

	return dataConnRecv, dataConnSend, dataConnServerAddr, nil
}

func downloadFile(name []byte) ( /*filedata*/ []byte, error) {

	// generate read req
	rrqBuff := tftputil.GetRequestBuffer(tftputil.RequestPacket{tftputil.RRQ_OPCODE, name, tftputil.OCTET_MODE})

	fileData := make([]byte, 0, 1024)

	dataConnRecv, dataConnSend, dataConnServerAddr, err := sendRequestAndEstablishDataConnection(rrqBuff)
	if err != nil {
		fmt.Println("error connecting to TFTP server.  Error: ", err)
		return fileData, err
	}
	defer dataConnRecv.Close()
	defer dataConnSend.Close()

	// receive data, send ack and wait for data transfer complete
	var blockId uint16 = 0
	dataRecvComplete := false

	const MAX_RETRY_COUNT = 10
	var retries = 0

	for retries < MAX_RETRY_COUNT {

		retries++

		// send ack for last successful data packet
		if blockId > 0 {
			ackBuff := tftputil.GetAckBuffer(tftputil.AckPacket{tftputil.ACK_OPCODE, blockId})

			_, err = dataConnSend.Write(ackBuff)
			if err != nil {
				fmt.Println("error connecting server for sending ack.  Error: ", err)
				continue
			}
		}

		if dataRecvComplete {
			break
		}

		// receive next data packet
		buf := make([]byte, 1024)
		dataConnRecv.SetReadDeadline(time.Now().Add(time.Second * 5))
		rlen, addr, err := dataConnRecv.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Error getting server data for block, err: ", blockId+1, err)
			continue
		}

		if addr.String() != dataConnServerAddr.String() {
			fmt.Println("data receive from server, expected server", addr, dataConnServerAddr)
			continue
		}

		var data tftputil.DataPacket
		data, err = tftputil.GetDataPacket(buf[0:rlen])
		if err != nil {
			fmt.Println("Error getting server data for block, err: ", blockId+1, err)
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
		//fmt.Println("name, block, total size received", string(name), blockId, len(fileData))

		// reset retry count
		retries = 0

		if len(data.Data) != tftputil.MAX_DATA_SIZE {
			// end of file
			dataRecvComplete = true

			fmt.Println("file received: name, size:", string(name), len(fileData))
		}

	}

	if dataRecvComplete {
		return fileData, nil
	} else {
		return fileData, errors.New("request terminated")
	}

}

func uploadFile(file []byte, name []byte) error {

	// generate write req
	var wrq = tftputil.RequestPacket{tftputil.WRQ_OPCODE, name, tftputil.OCTET_MODE}
	wrqBuff := tftputil.GetRequestBuffer(wrq)
	//fmt.Println("wrqBuff: ", wrqBuff)

	dataConnRecv, dataConnSend, dataConnServerAddr, err := sendRequestAndEstablishDataConnection(wrqBuff)
	if err != nil {
		fmt.Println("error connecting to TFTP server.  Error: ", err)
		return err
	}
	defer dataConnRecv.Close()
	defer dataConnSend.Close()

	var blockIdAcked uint16 = 0
	dataWriteComplete := false
	const MAX_RETRY_COUNT = 10
	var retries = 0

	for !dataWriteComplete && (retries < MAX_RETRY_COUNT) {
		retries++
		//fmt.Println("retry:", retries)

		// generate data packet
		var blockId uint16 = blockIdAcked + 1
		var startOffset int = int(blockIdAcked) * tftputil.MAX_DATA_SIZE
		endOffset := startOffset + tftputil.MAX_DATA_SIZE
		if endOffset > len(file) {
			endOffset = len(file)
		}
		var data []byte = file[startOffset:endOffset]
		packet := tftputil.DataPacket{tftputil.DATA_OPCODE, blockId, data}
		dataBuff := tftputil.GetDataBuffer(packet)

		_, err = dataConnSend.Write(dataBuff)
		if err != nil {
			fmt.Println("error sending block, Error: ", blockId, err)
			continue
		} else {
			//fmt.Println("Data sent blockId", blockId)
		}

		buf := make([]byte, 1024)
		dataConnRecv.SetReadDeadline(time.Now().Add(time.Second * 5))
		rlen, serverAddr, err := dataConnRecv.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Error getting server ack for request, err: ", err)
			continue
		}

		if serverAddr.String() != dataConnServerAddr.String() {
			fmt.Println("data receive from server, expected server", serverAddr, dataConnServerAddr)
			continue
		}

		var ack tftputil.AckPacket
		ack, err = tftputil.GetAckPacket(buf[0:rlen])
		if err != nil {
			fmt.Println("Error getting server ack for wr request, err: ", err)
			continue
		}

		if ack.BlockId != blockId {
			fmt.Println("ack receive for blockId, expected blockId ", ack.BlockId, blockId)
			continue
		}

		blockIdAcked = blockId
		//fmt.Println("received ack: blockId, serverAddr: ", blockIdAcked, serverAddr)

		// reset retry count
		retries = 0

		if (int(blockIdAcked) * tftputil.MAX_DATA_SIZE) > len(file) {
			dataWriteComplete = true
		}
	}

	if dataWriteComplete {
		return nil
	} else {
		return errors.New("request terminated")
	}

}

func uploadRandomFileAndVerifyDownload(size int, name []byte) error {

	file, err := getRandomByteBuffer(size)
	if err != nil {
		fmt.Println("error generating random file contents. ", err)
		return err
	}

	err = uploadFile(file, name)
	if err != nil {
		fmt.Println("error uploading file. ", err)
		return err
	}

	var fileData []byte
	fileData, err = downloadFile(name)
	if err != nil {
		fmt.Println("error downloading file. ", err)
		return err
	}

	if !bytes.Equal(file, fileData) {
		return errors.New("uploaded and downloaded file do not match")
	}

	return nil
}

//////////////////////////////////////////////////

func test1() {
	fmt.Println("Test Case Start: upload/download a file (not multiple of 512)")
	err := uploadRandomFileAndVerifyDownload(2000, []byte("test1"))
	if err != nil {
		fmt.Println("Test Case FAIL: upload/download a file (not multiple of 512)", err)
		return
	}
	fmt.Println("Test Case PASS: upload/download a file (not multiple of 512)")
}

func test2() {
	fmt.Println("Test Case Start: upload/download a file (multiple of 512)")
	err := uploadRandomFileAndVerifyDownload(2048, []byte("test2"))
	if err != nil {
		fmt.Println("Test Case FAIL: upload/download a file (multiple of 512)", err)
		return
	}
	fmt.Println("Test Case PASS: upload/download a file (multiple of 512)")
}

func test3() {
	fmt.Println("Test Case Start: upload/download a file of size 0")
	err := uploadRandomFileAndVerifyDownload(0, []byte("test3"))
	if err != nil {
		fmt.Println("Test Case FAIL: upload/download a file of size 0", err)
		return
	}
	fmt.Println("Test Case PASS: upload/download a file of size 0")
}

func test4() {
	fmt.Println("Test Case Start: upload/download a file of size 512")
	err := uploadRandomFileAndVerifyDownload(512, []byte("test4"))
	if err != nil {
		fmt.Println("Test Case FAIL: upload/download a file of size 512", err)
		return
	}
	fmt.Println("Test Case PASS: upload/download a file of size 512")
}

func test5() {
	fmt.Println("Test Case start: upload/download a file of size < 512")
	err := uploadRandomFileAndVerifyDownload(30, []byte("test5"))
	if err != nil {
		fmt.Println("Test Case FAIL: upload/download a file of size < 512", err)
		return
	}
	fmt.Println("Test Case PASS: upload/download a file of size < 512")
}

func test6() {
	fmt.Println("Test Case start: try download unknown file")

	_, err := downloadFile([]byte("test6"))
	if err == nil {
		fmt.Println("Test Case FAIL: try download unknown file")
		return
	}
	fmt.Println("Test Case PASS: try download unknown file")
}

func test7() {
	fmt.Println("Test Case start: try upload duplicate file name")

	var fileData []byte = make([]byte, 0)
	err := uploadFile(fileData, []byte("test5"))
	if err == nil {
		fmt.Println("Test Case FAIL: try upload duplicate file name")
		return
	}
	fmt.Println("Test Case PASS: try upload duplicate file name")
}

func test8() {
	fmt.Println("Test Case start: try upload file with very long file name (1KB)")

	var fileData []byte = make([]byte, 0)
	var fileName []byte
	fileName, _ = getRandomByteBuffer(1024)
	err := uploadFile(fileData, fileName)
	if err == nil {
		fmt.Println("Test Case FAIL: try upload file with very long file name")
		return
	}
	fmt.Println("Test Case PASS: try upload file with very long file name")
}

//////////////////////////////////////////////////

func main() {

	go test1()
	go test2()
	go test3()
	go test4()
	test5()
	go test6()
	test7()
	go test8()

	time.Sleep(time.Second * 60)

	//////////////////////////////////////////////////

}
