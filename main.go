package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ByLCY/papyrus/dsl"
	"github.com/ByLCY/papyrus/layout"
	"github.com/ByLCY/papyrus/renderer"
	canvasrenderer "github.com/ByLCY/papyrus/renderer/canvas"
)

func main() {
	input := flag.String("in", "examples/demo.papyrus", "DSL 文件路径")
	output := flag.String("out", "output/demo.pdf", "PDF 输出路径")
	debug := flag.String("debug", "", "布局调试 JSON 输出路径")
	debugRawUnits := flag.Bool("debug-raw-units", false, "在调试 JSON 中输出 debug.rawUnits 影子字段")
	dataJSON := flag.String("data", "", "绑定到 DSL 的 JSON 数据")
	flag.Parse()

	var inputData any
	if *dataJSON != "" {
		if err := json.Unmarshal([]byte(*dataJSON), &inputData); err != nil {
			log.Fatalf("解析 data JSON 失败: %v", err)
		}
	}

	var r renderer.Renderer = canvasrenderer.NewRenderer(filepath.Dir(*input))
	if err := run(*input, *output, *debug, *debugRawUnits, inputData, r); err != nil {
		log.Fatalf("生成 PDF 失败: %v", err)
	}
	fmt.Printf("已生成 PDF：%s\n", *output)
}

// run 串联解析、布局与渲染。
func run(inputPath, outputPath, debugPath string, debugRawUnits bool, data any, r renderer.Renderer) error {
	if r == nil {
		return fmt.Errorf("renderer 不能为空")
	}
	file, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("无法打开 DSL 文件 %s: %w", inputPath, err)
	}
	defer file.Close()

	doc, err := dsl.Parse(file)
	if err != nil {
		return fmt.Errorf("解析 DSL 失败: %w", err)
	}

	ts, ok := r.(layout.Typesetter)
	if !ok {
		return fmt.Errorf("renderer 未实现排版接口")
	}

 result, err := layout.Build(doc, data, layout.BuildOptions{
		Typesetter: ts,
		Debug:      layout.DebugOptions{RawUnits: debugRawUnits},
	})
	if err != nil {
		return fmt.Errorf("布局计算失败: %w", err)
	}

	if debugPath != "" {
		if err := writeDebug(result, debugPath); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	pdfBytes, err := r.Render(result)
	if err != nil {
		return fmt.Errorf("渲染 PDF 失败: %w", err)
	}
	if err := os.WriteFile(outputPath, pdfBytes, 0o644); err != nil {
		return fmt.Errorf("写入 PDF 文件失败: %w", err)
	}

	return nil
}

func writeDebug(result *layout.Result, debugPath string) error {
	if err := os.MkdirAll(filepath.Dir(debugPath), 0o755); err != nil {
		return fmt.Errorf("创建调试目录失败: %w", err)
	}
	if err := layout.WriteDebugJSON(result, debugPath); err != nil {
		return fmt.Errorf("输出调试 JSON 失败: %w", err)
	}
	return nil
}
