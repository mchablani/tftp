# tftp
tftp implementation
------------------


tftp.go is my TFTP server: binds and listens on port 69
test.go is my test clinet: implements test cases that talk to TFTP server
src/tftputil is the package that both server and client include for common code related to defining TFTP packets serializers and parsers.