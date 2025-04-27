package main

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	// "strconv"
	"sync"
	// "time"
	// "strings"
)

var (
	serverURL string
	savePath  string
	concurrency int
	resumeFile string
)

type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type DownloadState struct {
	Files map[string]int64 `json:"files"` // 文件路径 -> 已下载大小
}

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
	rootCmd.PersistentFlags().IntVarP(&concurrency, "concurrency", "c", 5, "下载并发数")
	rootCmd.PersistentFlags().StringVarP(&resumeFile, "resume", "r", ".download_state.json", "断点续传状态文件")

	rootCmd.AddCommand(downloadCmd)
	rootCmd.AddCommand(downloadDirCmd)
	rootCmd.AddCommand(listCmd)
}

func loadDownloadState() DownloadState {
	state := DownloadState{
		Files: make(map[string]int64),
	}
	data, err := os.ReadFile(resumeFile)
	if err == nil {
		json.Unmarshal(data, &state)
	}
	return state
}

func saveDownloadState(state DownloadState) {
	data, err := json.Marshal(state)
	if err == nil {
		os.WriteFile(resumeFile, data, 0644)
	}
}

func downloadFile(filePath string) {
	state := loadDownloadState()
	
	// URL编码文件路径
	encodedPath := url.PathEscape(filePath)
	url := fmt.Sprintf("%s/download/%s", serverURL, encodedPath)
	
	// 获取文件信息
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		return
	}
	
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("获取文件失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("服务器返回错误状态码: %d\n", resp.StatusCode)
		return
	}

	totalSize := resp.ContentLength
	if totalSize <= 0 {
		fmt.Printf("警告: 无法获取文件 %s 的大小，将继续下载\n", filePath)
	} else {
		fmt.Printf("开始下载: %s (大小: %.2f MB)\n", filePath, float64(totalSize)/1024/1024)
	}

	// 检查本地文件
	saveDir := filepath.Join(savePath, filepath.Dir(filePath))
	err = os.MkdirAll(saveDir, 0755)
	if err != nil {
		fmt.Printf("创建目录失败: %v\n", err)
		return
	}

	fileName := filepath.Join(savePath, filePath)
	if info, err := os.Stat(fileName); err == nil {
		if totalSize > 0 && info.Size() == totalSize {
			fmt.Printf("文件 %s 已存在且完整，跳过下载\n", filePath)
			return
		}
	}
	
	out, err := os.Create(fileName)
	if err != nil {
		fmt.Printf("创建文件失败: %v\n", err)
		return
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		fmt.Printf("保存文件失败: %v\n", err)
		return
	}

	if totalSize > 0 && written != totalSize {
		fmt.Printf("警告: 文件大小不匹配，预期 %.2f MB，实际 %.2f MB\n", 
			float64(totalSize)/1024/1024, 
			float64(written)/1024/1024)
		return
	}

	state.Files[filePath] = written
	saveDownloadState(state)
	fmt.Printf("成功下载: %s (%.2f MB)\n", fileName, float64(written)/1024/1024)
}

func downloadDirectory(dirPath string) {
	// URL编码目录路径
	encodedPath := url.PathEscape(dirPath)
	url := fmt.Sprintf("%s/list/%s", serverURL, encodedPath)
	
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		return
	}
	
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("获取目录列表失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("错误: 服务器返回状态码 %d\n", resp.StatusCode)
		return
	}

	var result struct {
		Files []FileInfo `json:"files"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		fmt.Printf("解析响应失败: %v\n", err)
		return
	}

	if len(result.Files) == 0 {
		fmt.Printf("目录 %s 为空或不存在\n", dirPath)
		return
	}

	// 创建任务通道
	tasks := make(chan FileInfo, len(result.Files))
	var wg sync.WaitGroup

	// 启动工作协程
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range tasks {
				downloadFile(file.Path)
			}
		}()
	}

	// 分发任务
	for _, file := range result.Files {
		tasks <- file
	}
	close(tasks)

	// 等待所有下载完成
	wg.Wait()
	fmt.Printf("目录下载完成: %s\n", dirPath)
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