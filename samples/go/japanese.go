package main

type Japanese struct{}

func (Japanese) Greet(name string) string {
	return "こんにちは、" + name
}
