package main

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"os"
	"path/filepath"
	// "strings"
)

var (
	serverURL string
	savePath  string
)

var rootCmd = &cobra.Command{
	Use:   "fileclient",
	Short: "A file download client",
}

var downloadCmd = &cobra.Command{
	Use:   "download [file_path]",
	Short: "Download a single file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		downloadFile(args[0])
	},
}

var downloadDirCmd = &cobra.Command{
	Use:   "downloaddir [dir_path]",
	Short: "Download a directory recursively",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		downloadDirectory(args[0])
	},
}

var listCmd = &cobra.Command{
	Use:   "list [dir_path]",
	Short: "List available directories on server",
	Run: func(cmd *cobra.Command, args []string) {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		listServerContent(path)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", "http://localhost:8080", "server URL")
	rootCmd.PersistentFlags().StringVarP(&savePath, "output", "o", ".", "save path")

	rootCmd.AddCommand(downloadCmd)
	rootCmd.AddCommand(downloadDirCmd)
	rootCmd.AddCommand(listCmd)
}

func downloadFile(filePath string) {
	url := fmt.Sprintf("%s/download/%s", serverURL, filePath)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error downloading file: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: server returned status %d\n", resp.StatusCode)
		return
	}

	// 创建保存目录
	saveDir := filepath.Join(savePath, filepath.Dir(filePath))
	err = os.MkdirAll(saveDir, 0755)
	if err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	// 创建文件
	fileName := filepath.Join(savePath, filePath)
	out, err := os.Create(fileName)
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer out.Close()

	// 写入文件
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		fmt.Printf("Error saving file: %v\n", err)
		return
	}

	fmt.Printf("Successfully downloaded: %s\n", fileName)
}

func downloadDirectory(dirPath string) {
	// 获取目录结构
	url := fmt.Sprintf("%s/list/%s", serverURL, dirPath)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error getting directory listing: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: server returned status %d\n", resp.StatusCode)
		return
	}

	var result struct {
		Files []string `json:"files"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		fmt.Printf("Error decoding response: %v\n", err)
		return
	}

	// 下载每个文件
	for _, file := range result.Files {
		if file == "" {
			continue
		}
		fmt.Printf("Downloading: %s\n", file)
		downloadFile(file)
	}

	fmt.Printf("Directory download completed: %s\n", dirPath)
}

func listServerContent(dirPath string) {
	url := fmt.Sprintf("%s/list/%s", serverURL, dirPath)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("获取目录列表出错: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("错误: 服务器返回状态码 %d\n", resp.StatusCode)
		return
	}

	var result struct {
		Files []string `json:"files"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		fmt.Printf("解析响应出错: %v\n", err)
		return
	}

	// 创建一个map来存储目录
	dirs := make(map[string]bool)
	
	// 提取目录
	for _, file := range result.Files {
		dir := filepath.Dir(file)
		if dir != "." {
			dirs[dir] = true
		}
	}

	// 打印目录列表
	fmt.Println("可用的目录:")
	for dir := range dirs {
		fmt.Printf("- %s\n", dir)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
} 