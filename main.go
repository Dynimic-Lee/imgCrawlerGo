package main

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/hunterhug/marmot/miner"
	"github.com/hunterhug/parrot/util"
	"github.com/metakeule/fmtdate"
	"github.com/pquerna/ffjson/ffjson"
	"golang.org/x/net/html"
)

// WorkNum . Num of Work, We can run it at the same time to crawl data fast
var WorkNum = 5

// ProxyAddress . You can update this decide whether to proxy
var ProxyAddress interface{}

type Config struct {
	SaveDir    string `json:"SaveDir"`
	StartIndex int    `json:"StartIndex"`
	ReadCount  int    `json:"ReadCount"`
	Ticker     int    `json:"Ticker"`
}

var Conf Config

func main() {
	var err error

	data, err := ioutil.ReadFile("./config.json")
	if err != nil {
		panic(err)
	}

	err = ffjson.Unmarshal(data, &Conf)
	if err != nil {
		panic(err)
	}

	startIndex := Conf.StartIndex
	count := Conf.ReadCount

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

	defer log.Printf("============= Exit =============")
	st, err := Crawling(file, startIndex, count)
	if err != nil {
		return
	}
	startIndex += st

	if Conf.Ticker > 0 {
		log.Printf("============= Ticker[%d] =============\n", Conf.Ticker)
		ticker := time.NewTicker(time.Hour * time.Duration(Conf.Ticker))
		for _ = range ticker.C {
			st, err = Crawling(file, startIndex, count)
			if err != nil {
				ticker.Stop()
			}
			startIndex += st
		}
	}
}

func Crawling(listFile *os.File, startIndex, count int) (int, error) {
	urls, err, cnt := PornFileRead(listFile, startIndex, count)
	if err != nil {
		log.Println(err.Error())
		return 0, err
	}

	if cnt == 0 {
		log.Println("file all clear")
		return 0, errors.New("file all clear")
	}

	log.Printf("======== [%d]->[%d] Crawling Start =========", startIndex, startIndex+count-1)

	saveDir := fmt.Sprintf("%s/%s", Conf.SaveDir, fmtdate.Format(fmtdate.DefaultDateFormat, time.Now()))

	// Make dir!
	err = util.MakeDir(saveDir)
	if err != nil {
		panic(err)
	}

	var saveCnt int
	for i, url := range urls {
		log.Printf("[%d] Find Start [%s]", startIndex+i, url)
		succCnt, err := CatchPicture(url, saveDir)
		if err != nil {
			log.Println("Error :" + err.Error())
		}
		saveCnt += succCnt
	}

	log.Printf("======== Save : %d ========", saveCnt)
	startIndex += count
	return startIndex, nil
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

	// New a worker to get url
	worker, _ := miner.New(ProxyAddress)

	if false == strings.HasPrefix(pictureURL, "http") {
		URL = "http://" + URL
	}

	result, err := worker.SetUrl(URL).SetUa(miner.RandomUa()).Get()
	if err != nil {
		if true == strings.HasPrefix(pictureURL, "http://") {
			URL = strings.Replace(URL, "http", "https", 1)
		} else if true == strings.HasPrefix(pictureURL, "https://") {
			URL = strings.Replace(URL, "https", "http", 1)
		}

		result, err = worker.SetUrl(URL).SetUa(miner.RandomUa()).Get()
		if err != nil {
			log.Println(err.Error())
			return 0, err
		}
	}

	// Find all picture URL
	pictures := GetImgTag(string(result))

	// Empty, What a pity!
	if len(pictures) == 0 {
		return 0, fmt.Errorf("[%s] Empty Picutres link", pictureURL)
	}

	// Devide pictures into several worker
	xxx, _ := util.DevideStringList(pictures, WorkNum)

	// Chanel to info exchange
	chs := make(chan int, len(pictures))

	// Go at the same time
	for num, imgs := range xxx {
		// fmt.Printf("[CatchPicture] [%d][%d]", num, len(imgs))
		// Get pool miner
		workerPicture, ok := miner.Pool.Get(util.IS(num))
		if !ok {
			// No? set one!
			workerTemp, err := miner.New(ProxyAddress)
			if err != nil {
				log.Println(err.Error())
				continue
			}
			workerPicture = workerTemp
			workerTemp.SetUa(miner.RandomUa())
			miner.Pool.Set(util.IS(num), workerTemp)
		}

		// Go save picture!
		go func(imgs []string, worker *miner.Worker, num int) {
			for _, img := range imgs {

				// Check, May be Pass
				_, err := url.Parse(img)
				if err != nil {
					chs <- 0
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
					imgsrc, e := worker.SetUrl(img).Get()
					if e != nil {
						// log.Println("Download " + img + " error:" + e.Error())
						chs <- 0
						continue
					}

					// Save it!
					e = util.SaveToFile(dir+"/"+filename, imgsrc)
					if e != nil {
						log.Printf("Image Save Fail %s/%s [%s]\n", dir, filename, e.Error())
						chs <- 0
						continue
					}
					chs <- 1
				}
			}
		}(imgs, workerPicture, num)
	}

	succ := 0
	// Every picture should return
	for i := 0; i < len(pictures); i++ {
		fmt.Printf("[%d/%d] ", i+1, len(pictures))
		if 1 == <-chs {
			succ++
		}
	}
	fmt.Println()

	log.Printf("[%d/%d] Complate %s\n", succ, len(pictures), pictureURL)

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
