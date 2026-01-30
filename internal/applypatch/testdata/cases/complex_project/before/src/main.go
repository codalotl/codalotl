package main

import "fmt"

func main() {
	fmt.Println("hello from before")
	helper()
}

func helper() {
	fmt.Println("helper")
}
