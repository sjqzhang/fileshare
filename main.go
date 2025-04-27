package main

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 服务端变量
var (
	port     string
	rootPath string
)

// 客户端变量
var (
	serverURL   string
	savePath    string
	concurrency int
	resumeFile  string
)

// 客户端数据结构
type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type DownloadState struct {
	Files map[string]int64 `json:"files"`
}

var rootCmd = &cobra.Command{
	Use:   "fileshare",
	Short: "文件共享工具 - 支持服务端和客户端功能",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动文件服务器",
	Run: func(cmd *cobra.Command, args []string) {
		startServer()
	},
}

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "启动文件客户端",
}

var downloadCmd = &cobra.Command{
	Use:   "download [file_path]",
	Short: "下载单个文件",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		downloadFile(args[0])
	},
}

var downloadDirCmd = &cobra.Command{
	Use:   "downloaddir [dir_path]",
	Short: "递归下载整个目录",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		downloadDirectory(args[0])
	},
}

var listCmd = &cobra.Command{
	Use:   "list [dir_path]",
	Short: "列出服务器上的可用目录",
	Run: func(cmd *cobra.Command, args []string) {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		listServerContent(path)
	},
}

func init() {
	// 服务端参数
	serverCmd.PersistentFlags().StringVarP(&port, "port", "p", "8080", "服务器端口")
	serverCmd.PersistentFlags().StringVarP(&rootPath, "path", "d", ".", "根目录路径")

	// 客户端参数
	clientCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", "http://localhost:8080", "服务器URL")
	clientCmd.PersistentFlags().StringVarP(&savePath, "output", "o", ".", "保存路径")
	clientCmd.PersistentFlags().IntVarP(&concurrency, "concurrency", "c", 5, "下载并发数")
	clientCmd.PersistentFlags().StringVarP(&resumeFile, "resume", "r", ".download_state.json", "断点续传状态文件")

	// 添加命令
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(clientCmd)
	clientCmd.AddCommand(downloadCmd)
	clientCmd.AddCommand(downloadDirCmd)
	clientCmd.AddCommand(listCmd)
}

// 服务端函数
func startServer() {
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		fmt.Printf("获取绝对路径失败: %v\n", err)
		return
	}

	r := gin.Default()

	// 处理单个文件下载
	r.GET("/download/*path", func(c *gin.Context) {
		filePath := c.Param("path")
		// 移除开头的斜杠并解码路径
		filePath = filePath[1:]
		decodedPath, err := url.QueryUnescape(filePath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的文件路径"})
			return
		}
		
		fullPath := filepath.Join(absPath, decodedPath)

		// 安全检查：确保路径不会超出根目录
		if !strings.HasPrefix(fullPath, absPath) {
			c.JSON(http.StatusForbidden, gin.H{"error": "访问被拒绝"})
			return
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "文件未找到"})
			return
		}

		if info.IsDir() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "不能直接下载目录，请使用 /list 接口"})
			return
		}

		c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))
		c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		http.ServeFile(c.Writer, c.Request, fullPath)
	})

	// 处理目录列表
	r.GET("/list/*path", func(c *gin.Context) {
		dirPath := c.Param("path")
		// 移除开头的斜杠并解码路径
		dirPath = dirPath[1:]
		decodedPath, err := url.QueryUnescape(dirPath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的目录路径"})
			return
		}
		
		fullPath := filepath.Join(absPath, decodedPath)

		// 安全检查：确保路径不会超出根目录
		if !strings.HasPrefix(fullPath, absPath) {
			c.JSON(http.StatusForbidden, gin.H{"error": "访问被拒绝"})
			return
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "目录未找到"})
			return
		}

		if !info.IsDir() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "不是目录"})
			return
		}

		var files []struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		}

		err = filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			relPath, _ := filepath.Rel(absPath, path)
			if !info.IsDir() {
				files = append(files, struct {
					Path string `json:"path"`
					Size int64  `json:"size"`
				}{
					Path: relPath,
					Size: info.Size(),
				})
			}
			return nil
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"files": files})
	})

	// 设置 gin 为发布模式
	gin.SetMode(gin.ReleaseMode)
	fmt.Printf("服务器启动在端口 %s，服务目录：%s\n", port, absPath)
	r.Run(":" + port)
}

// 客户端函数
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
	
	encodedPath := url.PathEscape(filePath)
	url := fmt.Sprintf("%s/download/%s", serverURL, encodedPath)
	
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

	tasks := make(chan FileInfo, len(result.Files))
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range tasks {
				downloadFile(file.Path)
			}
		}()
	}

	for _, file := range result.Files {
		tasks <- file
	}
	close(tasks)

	wg.Wait()
	fmt.Printf("目录下载完成: %s\n", dirPath)
}

func listServerContent(dirPath string) {
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

	dirs := make(map[string]bool)
	for _, file := range result.Files {
		dir := filepath.Dir(file.Path)
		if dir != "." {
			dirs[dir] = true
		}
	}

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