package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"net/http"
	"os"
	"path/filepath"
)

var (
	port     string
	rootPath string
)

var rootCmd = &cobra.Command{
	Use:   "fileserver",
	Short: "A file download server",
	Run: func(cmd *cobra.Command, args []string) {
		startServer()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&port, "port", "p", "8080", "server port")
	rootCmd.PersistentFlags().StringVarP(&rootPath, "path", "d", ".", "root directory path")
}

func startServer() {
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		fmt.Printf("Error getting absolute path: %v\n", err)
		return
	}

	r := gin.Default()

	// 处理单个文件下载
	r.GET("/download/*path", func(c *gin.Context) {
		filePath := c.Param("path")
		fullPath := filepath.Join(absPath, filePath)

		// 检查文件是否存在
		info, err := os.Stat(fullPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
			return
		}

		// 如果是目录，返回错误
		if info.IsDir() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot download directory directly, use /downloaddir endpoint"})
			return
		}

		c.File(fullPath)
	})

	// 处理目录下载（返回目录结构）
	r.GET("/list/*path", func(c *gin.Context) {
		dirPath := c.Param("path")
		fullPath := filepath.Join(absPath, dirPath)

		info, err := os.Stat(fullPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Directory not found"})
			return
		}

		if !info.IsDir() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Not a directory"})
			return
		}

		var files []string
		err = filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			relPath, _ := filepath.Rel(absPath, path)
			files = append(files, relPath)
			return nil
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"files": files})
	})

	fmt.Printf("Server starting on port %s, serving files from %s\n", port, absPath)
	r.Run(":" + port)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
} 