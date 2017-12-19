package main

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hunterhug/GoSpider/spider"
	"github.com/hunterhug/GoTool/util"
	"github.com/metakeule/fmtdate"
	"golang.org/x/net/html"
)

// SpiderNum . Num of spider, We can run it at the same time to crawl data fast
var SpiderNum = 5

// ProxyAddress . You can update this decide whether to proxy
var ProxyAddress interface{}

const dir = "./picture"
const resultFile = "./result.txt"

func main() {
	startIndex := 0
	count := 0
	var err error

	argsWithoutProg := os.Args[1:]
	switch len(argsWithoutProg) {
	case 1:
		count, err = strconv.Atoi(argsWithoutProg[0])
		if err != nil {
			log.Printf("argument convert error : %s", err.Error())
			return
		}
	case 2:
		startIndex, err = strconv.Atoi(argsWithoutProg[0])
		if err != nil {
			log.Printf("argument convert error : %s", err.Error())
			return
		}
		count, err = strconv.Atoi(argsWithoutProg[1])
		if err != nil {
			log.Printf("argument convert error : %s", err.Error())
			return
		}
	default:
		log.Println("argument is empty")
	}

	fpLog, err := os.OpenFile("log.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	defer fpLog.Close()

	multiWriter := io.MultiWriter(fpLog, os.Stdout)
	log.SetOutput(multiWriter)

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// 파일 오픈
	file, err := os.Open("./PornWebSite_100k.csv")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// 24시간마다 반복..
	ticker := time.NewTicker(time.Hour * 24)
	for _ = range ticker.C {
		urls, err, cnt := PornFileRead(file, startIndex, count)
		if err != nil {
			log.Println(err.Error())
			return
		}

		if cnt == 0 {
			log.Println("file all clear")
			ticker.Stop()
		}

		saveDir := fmt.Sprintf("%s/%s", dir, fmtdate.Format(fmtdate.DefaultDateFormat, time.Now()))

		var saveCnt int
		for _, url := range urls {
			succCnt, err := CatchPicture(url, saveDir)
			if err != nil {
				log.Println("Error:" + err.Error())
			}
			saveCnt += succCnt
		}

		log.Printf("=============Save : %d=============", saveCnt)
		startIndex += count
	}
}

// CatchPicture .
func CatchPicture(pictureURL string, dir string) (int, error) {
	URL := pictureURL

	// Check valid
	_, err := url.Parse(URL)
	if err != nil {
		log.Println(err.Error())
		return 0, err
	}

	// Make dir!
	err = util.MakeDir(dir)
	if err != nil {
		log.Println(err.Error())
		return 0, err
	}

	// New a sp to get url
	sp, _ := spider.New(ProxyAddress)

	if false == strings.HasPrefix(pictureURL, "http") {
		URL = "http://" + URL
	}

	log.Printf("========Find Start [%s]=========", URL)

	result, err := sp.SetUrl(URL).SetUa(spider.RandomUa()).Get()
	if err != nil {
		if true == strings.HasPrefix(pictureURL, "http://") {
			URL = strings.Replace(URL, "http", "https", 1)
		} else if true == strings.HasPrefix(pictureURL, "https://") {
			URL = strings.Replace(URL, "https", "http", 1)
		}

		result, err = sp.SetUrl(URL).SetUa(spider.RandomUa()).Get()
		if err != nil {
			log.Println(err.Error())
			return 0, err
		}
	}

	// Find all picture URL
	pictures := GetImgTag(string(result))

	// Empty, What a pity!
	if len(pictures) == 0 {
		return 0, errors.New(fmt.Sprintf("[%s] Empty Picutres link\n", URL))
	}

	// Devide pictures into several sp
	xxx, _ := util.DevideStringList(pictures, SpiderNum)

	// Chanel to info exchange
	chs := make(chan int, len(pictures))

	// Go at the same time
	for num, imgs := range xxx {

		// Get pool spider
		spPicture, ok := spider.Pool.Get(util.IS(num))
		if !ok {
			// No? set one!
			spTemp, err := spider.New(ProxyAddress)
			if err != nil {
				log.Println(err.Error())
				continue
			}
			spPicture = spTemp
			spTemp.SetUa(spider.RandomUa())
			spider.Pool.Set(util.IS(num), spTemp)
		}

		// Go save picture!
		go func(imgs []string, sp *spider.Spider, num int) {
			for _, img := range imgs {

				// Check, May be Pass
				_, err := url.Parse(img)
				if err != nil {
					continue
				}

				// Change Name of our picture
				filename := strings.Replace(util.ValidFileName(img), "#", "_", -1)

				// Exist?
				if util.FileExist(dir + "/" + filename) {
					// log.Println("File Exist：" + dir + "/" + filename)
					chs <- 0
				} else {

					// Not Exsit?
					imgsrc, e := sp.SetUrl(img).Get()
					if e != nil {
						// log.Println("Download " + img + " error:" + e.Error())
						chs <- 0
						continue
					}

					// Save it!
					e = util.SaveToFile(dir+"/"+filename, imgsrc)
					if e != nil {
						// log.Printf("Image Save Fail %s/%s [%s]\n", dir, filename, err.Error())
						chs <- 0
						continue
					}
					chs <- 1
				}
			}
		}(imgs, spPicture, num)
	}

	succ := 0
	// Every picture should return
	for i := 0; i < len(pictures); i++ {
		//fmt.Printf("%d / %d", i, len(pictures))
		if 1 == <-chs {
			succ++
		}
	}

	log.Printf("[%d/%d] Complate %s\n", succ, len(pictures), URL)

	return succ, nil
}

func GetImgTag(htmlstr string) []string {
	var imgs []string

	hdata := strings.Replace(string(htmlstr), "<noscript>", "", -1)
	hdata = strings.Replace(hdata, "</noscript>", "", -1)
	// --------------

	if document, err := html.Parse(strings.NewReader(hdata)); err == nil {
		var parser func(*html.Node)
		parser = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "img" {
				for _, element := range n.Attr {
					if element.Key == "src" {
						imgs = append(imgs, element.Val)
						// log.Println("Found src:", element.Val)
					}
				}
			}

			for c := n.FirstChild; c != nil; c = c.NextSibling {
				parser(c)
			}

		}
		parser(document)
	} else {
		log.Panicln("Parse html error", err)
	}

	return FindPictureName(imgs)
}

func FindPictureName(str []string) []string {
	returnlist := []string{}
	re, _ := regexp.Compile(`(http[s]?:\/\/.*?\.(jpg|jpeg))`)
	for _, s := range str {
		output := re.FindAllStringSubmatch(s, -1)
		for _, o := range output {
			returnlist = append(returnlist, o[1])
		}
	}

	return returnlist
}

func PornFileRead(file *os.File, start, cnt int) ([]string, error, int) {
	if 0 > start {
		start = 0
	}
	if 0 > cnt {
		cnt = 1
	}

	// csv reader 생성
	rdr := csv.NewReader(bufio.NewReader(file))

	// csv 내용 모두 읽기
	rows, _ := rdr.ReadAll()

	count := 0
	var ret []string
	// 행,열 읽기
	for i, row := range rows {
		if start <= i {
			// fmt.Printf("%d : %s\n", i, row[1])
			ret = append(ret, row[1])

			count++
			if count == cnt {
				break
			}
		}
	}

	return ret, nil, count
}
