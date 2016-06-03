package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
)

//cli命令行结构体
var args struct {
	LocalPort      int    `short:"p" long:"port" description:"localhost listen port" required:"true"`
	RemoteHostPort string `short:"r" long:"remote" description:"remote ip:port" required:"true"`
	DebugPort      int    `short:"d" long:"debug" description:"debug port" default:"6060"`
	Help           bool   `short:"h" long:"help" descrition:"the help message"`
}

var parser *flags.Parser

//检查args
func parseArgs(args interface{}) (e error) {
	_, err := parser.ParseArgs(os.Args)

	if err != nil {
		e = err
	}
	return
}

//在cli中输出命令配置
func printUsage() {
	parser.WriteHelp(os.Stderr)
}

// 初始化函数
func init() {
	parser = flags.NewParser(&args, flags.None)
	parser.Usage = "tcp-proxy -p port -r host:port [other opts]"
}

//错误输出函数
func printError(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format, a...)
	fmt.Printf("\n")
}

// main函数 不多说
func main() {

	var err error
	err = parseArgs(&args)

	if err != nil {
		printUsage()
		return
	}
	//使用go关键字实现debug服务器－http服务
	go func() {
		err := http.ListenAndServe(fmt.Sprintf(":%d", args.DebugPort), nil)
		if err != nil {
			printError(err.Error())
			printUsage()
			os.Exit(1)
		}
	}()
	//建立tcp服务
	localBinding := fmt.Sprintf(":%d", args.LocalPort)
	listener, err := net.Listen("tcp", localBinding)
	if err != nil {
		printError(err.Error())
		printUsage()
		return
	}
	//	timeout := 5 * time.Minute
	//完成请求到后端服务器的数据交接
	i := 1
	for {
		i++
		conn, err := listener.Accept()

		if err != nil {
			printError(err.Error())
		}
		//logs an incoming message
		fmt.Printf("[%d] Received message %s -> %s \n", i, conn.RemoteAddr(), conn.LocalAddr())

		buf := make([]byte, 1024)
		var nr int
		var er error
		for {
			nr, er = conn.Read(buf)
			//			fmt.Println("nr len:", nr, er)
			if nr > 0 {
				buf = buf[:nr]
				break
			}

			if er == io.EOF {
				buf = buf[:nr]
				break
			}

			if er != nil {
				buf = buf[:nr]
				break
			}
		}

		r := new(Request)
		if nr > 0 {
			rb := r.Format(buf).Check()
			if rb == false {
				send_date := time.Now().Format(time.RFC1123)
				httpStr := "404 page not found"

				err := "HTTP/1.1 200 OK\nData: " + send_date + "\nServer: HRWebSer/1.1\n"
				err = err + fmt.Sprintf("Content-Length: %d\n", len(httpStr))
				err = err + "\n"
				err = err + httpStr + "\n"
				err = err + "\r\n"
				errBuf := []byte(err)
				cliWriteBuffer(conn, errBuf)
			}
			// go proxyConn(conn, args.RemoteHostPort, buf) //请求到后端服务器的数据交接
		}
		go proxyConn(conn, args.RemoteHostPort, buf) //请求到后端服务器的数据交接

	}
}

//使用协程方式进行服务间数据的交换
func proxyConn(cliConn net.Conn, addr string, buf []byte) {

	serConn, err := net.Dial("tcp", addr)
	if err != nil {
		printError(err.Error())
		cliConn.Write([]byte(err.Error()))
	}

	forwardConn(cliConn, serConn, buf)
	//	if err != nil {
	//		printError(err.Error())
	//		//cliConn.Write([]byte(err.Error()))
	//	}

	serConn.Close()
	cliConn.Close()
}

func cliWriteBuffer(cli net.Conn, buf []byte) {
	//defer cli.Close()
	wbuf := buf
	for {
		if len(wbuf) == 0 {
			break
		}

		if n, err := cli.Write(wbuf); err != nil {
			if err == io.EOF {
				break
			}
			break
		} else {
			wbuf = wbuf[n:]
		}
	}
	cli.Close()
}

type Request struct {
	Method string // "GET,POST,PUT..."
	URL    string // "/a/b/c/?c=index&a=info"
	Proto  string // "HTTP/1.0"
	Host   string // "localhost:9501"
	//Hander   map[string][]string // "send[1:]"
	//PostData url.Values          // "Post Data"
}

func (r *Request) Format(buf []byte) *Request {

	sendData := string(buf)
	//	fmt.Println("sendData:", sendData)
	datas := strings.Split(sendData, "\r\n")
	//	for k, v := range datas {
	//		fmt.Printf("datas key %d : ver %s \n", k, v)
	//	}

	//	l := len(datas)
	//	HanderDatas := datas[2:(l - 2)]
	_m := strings.Split(datas[0], " ")
	r.Method = _m[0]
	r.URL = _m[1]
	r.Proto = _m[2]
	_h := strings.Split(datas[1], ": ")
	r.Host = _h[1]
	return r
}

// 此处代码为测试代码 后期需要修改
func (r *Request) Check() bool {
	var c_bool bool
	if r.URL == "/?c=index&a=test" {
		c_bool = true
	} else if r.URL == "/?c=index&a=info" {
		c_bool = false
	}
	return c_bool
}

func forwardConn(sConn, oConn net.Conn, buf []byte) error {
	errChan := make(chan error, 10)

	go sendToBuffer(oConn, buf, errChan)
	go _forwardConn(oConn, sConn, errChan)

	return <-errChan
}

func _forwardConn(sConn, oConn net.Conn, errChan chan error) {
	forwardBufSize := 32 * 1024
	buf := make([]byte, forwardBufSize)
	for {
		// 虽然存在 WriteTo 等方法，但是由于无法刷新超时时间，所以还是需要使用标准的 Read、Write。
		n, err := sConn.Read(buf)
		if err == io.EOF {
			errChan <- err
			break
		}
		if err != nil {
			errChan <- fmt.Errorf("转发读错误：%v", err)
			break
		} else {
			buf = buf[:n]
		}

		wbuf := buf
		for {
			if len(wbuf) == 0 {
				break
			}
			n, err := oConn.Write(wbuf)
			wbuf = wbuf[:n]
			if err == io.EOF {
				errChan <- err
				break
			}
			if err != nil {
				errChan <- fmt.Errorf("转发写错误：%v", err)
				break
			}
		}
	}
}

func sendToBuffer(dst net.Conn, buf []byte, errChan chan error) {
	for {
		wbuf := buf
		if len(wbuf) == 0 {
			break
		}
		n, err := dst.Write(wbuf)
		wbuf = wbuf[:n]
		if err == io.EOF {
			errChan <- err
			break
		}

		if err != nil {
			errChan <- fmt.Errorf("转发写错误：%v", err)
			break
		}

	}
}
