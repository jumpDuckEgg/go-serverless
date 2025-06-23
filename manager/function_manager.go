package manager

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"go-serverless/model"
	"io"
	"mime/multipart"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	functionStore = make(map[string]*model.Function)
	storeLock     sync.RWMutex
)

func ListFunctions() []*model.Function {
	storeLock.RLock()
	defer storeLock.RUnlock()
	var functions []*model.Function

	for _, function := range functionStore {
		functions = append(functions, function)
	}
	return functions
}

func GetFunction(id string) (*model.Function, error) {
	storeLock.RLock()
	defer storeLock.RUnlock()
	function, ok := functionStore[id]
	if !ok {
		return nil, errors.New("function not found")
	}
	return function, nil
}

func DeleteFunction(id string) error {
	storeLock.Lock()
	defer storeLock.Unlock()
	function, ok := functionStore[id]
	if !ok {
		return errors.New("function not found")
	}
	err := os.RemoveAll(filepath.Dir(function.BinPath))

	if err != nil {
		return err
	}

	delete(functionStore, id)
	return nil
}

func RegisterFunction(name string, file multipart.File, version string, ext string) (*model.Function, error) {
	id := uuid.NewString()

	fnDir := filepath.Join("functions", name, version)
	if err := os.MkdirAll(fnDir, 0755); err != nil {
		return nil, err
	}

	var binPath string

	if ext == ".zip" {
		// 保存 zip 到本地临时路径
		zipPath := filepath.Join(fnDir, "src.zip")
		out, err := os.Create(zipPath)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, file); err != nil {
			out.Close()
			return nil, err
		}
		out.Close()

		if err := unzip(zipPath, fnDir); err != nil {
			return nil, err
		}

		if err != nil {
			return nil, err
		}

		mainDir, err := findMainGoDir(fnDir)
		if err != nil {
			return nil, err
		}

		if err := ensureGoMod(mainDir); err != nil {
			return nil, err
		}

		absFnDir, err := filepath.Abs(fnDir) // 保证用绝对路径
		if err != nil {
			return nil, err
		}

		binPath = filepath.Join(absFnDir, "main.bin")

		err = buildFunction(mainDir, binPath)

		if err != nil {
			return nil, err
		}

	} else if ext == ".go" {
		srcPath := filepath.Join(fnDir, "main.go")
		out, err := os.Create(srcPath)
		if err != nil {
			return nil, err
		}

		if _, err := io.Copy(out, file); err != nil {
			return nil, err
		}
		defer out.Close()

		binPath = filepath.Join(fnDir, "main.bin")

		cmd := exec.Command("go", "build", "-o", binPath, srcPath)

		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64")

		buildOut, err := cmd.CombinedOutput()
		if err != nil {
			return nil, errors.New("go build error: " + string(buildOut))
		}
	} else {
		// 假如上传的是二进制文件
		binPath = filepath.Join(fnDir, "main.bin")
		out, err := os.Create(binPath)
		if err != nil {
			return nil, err
		}

		if _, err := io.Copy(out, file); err != nil {
			return nil, err
		}
		defer out.Close()
	}

	fn := &model.Function{
		ID:          id,
		Version:     version,
		Name:        name,
		BinPath:     binPath,
		CreatedAt:   time.Now(),
		Description: "",
	}

	saveFunctionMeta(fn)

	storeLock.Lock()
	functionStore[id] = fn
	storeLock.Unlock()
	return fn, nil
}

// 解压 zip 包到目标目录
// 接收 zip 文件路径和解压目标目录
// 遍历 zip 包里的每个文件
// 依次将其内容解压到目标目录下
func unzip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return errors.New("illegal file path in zip: " + fpath)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, f.Mode())
			continue
		}
		if err = os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())

		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func saveFunctionMeta(fn *model.Function) error {
	storeLock.Lock()
	defer storeLock.Unlock()

	metaPath := filepath.Join(filepath.Dir(fn.BinPath), "meta.json")
	data, err := json.MarshalIndent(fn, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(metaPath, data, 0644)
}

// 找到 main.go 所在目录，如果没找到返回空字符串
func findMainGoDir(root string) (string, error) {
	var mainDir string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() == "main.go" {
			mainDir = filepath.Dir(path)
			return filepath.SkipDir // 找到就跳出
		}
		return nil
	})
	if mainDir == "" {
		return "", errors.New("main.go not found in zip")
	}
	return mainDir, err
}

func LoadAllFunctions(baseDir string) error {
	storeLock.Lock()
	defer storeLock.Unlock()

	files, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 如果目录不存在，直接返回
		}
		return err
	}

	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		fnDir := filepath.Join(baseDir, file.Name())
		metaPath := filepath.Join(fnDir, "meta.json")
		binPath := filepath.Join(fnDir, "main.bin")
		if _, err := os.Stat(binPath); err != nil {
			continue // 如果没有 main.bin 文件，跳过这个目录
		}

		if _, err := os.Stat(metaPath); err != nil {
			continue // 如果没有 meta.json 文件，跳过这个目录
		}

		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue // 如果读取 meta.json 失败，跳过这个目录
		}

		var fn model.Function
		if err := json.Unmarshal(data, &fn); err != nil {
			continue // 如果解析失败，跳过这个目录
		}

		fn.BinPath = binPath // 确保 BinPath 正确
		functionStore[fn.ID] = &fn
	}
	return nil
}

func buildFunction(mainDir, binPath string) error {
	// go mod download
	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = mainDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New("go mod download failed: " + err.Error() + "\n" + string(out))
	}

	// go build
	cmd = exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = mainDir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")

	buildOut, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("build failed: " + err.Error() + "\n" + string(buildOut))
	}
	return nil
}

func ensureGoMod(dir string) error {
	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		// 自动生成
		modContent := []byte("module example.com/tmpmod\n\ngo 1.20\n")
		return os.WriteFile(goModPath, modContent, 0644)
	}
	return nil
}
