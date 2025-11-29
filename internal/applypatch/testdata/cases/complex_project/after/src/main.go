package main

import "fmt"

func main() {
	fmt.Println("hello from after")
	helper()
}

func helper() {
	fmt.Println("helper updated")
}
