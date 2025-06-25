package manager

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
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

	var binPath, wasmPath string

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

		// 新增：用 tinygo 生成 wasm
		wasmPath = filepath.Join(absFnDir, "main.wasm")
		buildWasmErr := buildWasmFunction(mainDir, wasmPath)
		if buildWasmErr != nil {
			// 生成 wasm 失败不致命，只记录日志
			fmt.Println("tinygo build wasm error:", buildWasmErr)
			wasmPath = ""
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

		// 1. 自动生成 go.mod（如果不存在）
		goModPath := filepath.Join(fnDir, "go.mod")
		if _, err := os.Stat(goModPath); os.IsNotExist(err) {
			modContent := []byte("module example.com/tmpmod\n\ngo 1.20\n")
			if err := os.WriteFile(goModPath, modContent, 0644); err != nil {
				return nil, err
			}
		}

		// 2. 先 go mod tidy（可选，让依赖全自动下载）
		cmd := exec.Command("go", "mod", "tidy")
		cmd.Dir = fnDir
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64")
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, errors.New("go mod tidy error: " + string(out))
		}

		absFnDir, err := filepath.Abs(fnDir) // 保证用绝对路径

		binPath = filepath.Join(absFnDir, "main.bin")

		cmd = exec.Command("go", "build", "-o", binPath, srcPath)

		cmd.Env = append(os.Environ(), "GO111MODULE=on")

		buildOut, err := cmd.CombinedOutput()
		if err != nil {
			return nil, errors.New("go build error: " + string(buildOut))
		}
		// 新增：用 tinygo 生成 wasm
		wasmPath = filepath.Join(absFnDir, "main.wasm")
		buildWasmErr := buildWasmFunction(fnDir, wasmPath)
		if buildWasmErr != nil {
			fmt.Println("tinygo build wasm error:", buildWasmErr)
			wasmPath = ""
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
		WasmPath:    wasmPath,
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

	funcDirs, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 如果目录不存在，直接返回
		}
		return err
	}

	for _, funcDir := range funcDirs {
		if !funcDir.IsDir() {
			continue
		}
		funcNameDir := filepath.Join(baseDir, funcDir.Name())

		// 第二层（版本）目录
		versionDirs, err := os.ReadDir(funcNameDir)
		if err != nil {
			continue // 可能不是目录
		}
		for _, versionDir := range versionDirs {
			if !versionDir.IsDir() {
				continue
			}
			fnDir := filepath.Join(funcNameDir, versionDir.Name())
			metaPath := filepath.Join(fnDir, "meta.json")
			binPath := filepath.Join(fnDir, "main.bin")
			wasmPath := filepath.Join(fnDir, "main.wasm")
			if _, err := os.Stat(binPath); err != nil {
				continue // 没有 main.bin 跳过
			}
			if _, err := os.Stat(metaPath); err != nil {
				continue // 没有 meta.json 跳过
			}

			data, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}

			var fn model.Function
			if err := json.Unmarshal(data, &fn); err != nil {
				continue
			}

			fn.BinPath = binPath
			if _, err := os.Stat(wasmPath); err == nil {
				fn.WasmPath = wasmPath
			}
			functionStore[fn.ID] = &fn
		}
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

// 新增：用 tinygo 构建 wasm
func buildWasmFunction(mainDir, wasmPath string) error {
	cmd := exec.Command("tinygo", "build", "-o", wasmPath, "-target=wasi", ".")
	cmd.Dir = mainDir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tinygo build wasm error: %v\n%s", err, string(out))
	}
	return nil
}
