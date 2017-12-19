package main

import (
	"testing"
)

func TestCSVRead(t *testing.T) {
	PornFileRead("C:\\GOAPP\\src\\webImgCrawler\\PornWebSite_100k.csv", 0, 10)
	PornFileRead("C:\\GOAPP\\src\\webImgCrawler\\PornWebSite_100k.csv", 10, 10)
}
