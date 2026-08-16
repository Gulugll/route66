package main

import (
	"log"

	"awesomeProject/internal/api"
	"awesomeProject/internal/config"
	"awesomeProject/internal/matrix"
)

// main 是装配层:读配置、拼好各包、启动。只在这里做"接线",不写业务逻辑。
func main() {
	cfg := config.Load()

	m := matrix.New() // 下一阶段:传入高德 client + Redis
	r := api.NewRouter(m)

	log.Printf("listening on :%s", cfg.Port)
	log.Fatal(r.Run(":" + cfg.Port))
}
