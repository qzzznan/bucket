package main

import (
	"bytes"
	cc "encoding/csv"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"text/template"
)

const dir = "./csv"
const outputDir = "./csv/output"

func main() {
	_ = os.Mkdir(outputDir, os.ModePerm)

	fset := token.NewFileSet()
	files, err := parser.ParseDir(fset, dir, nil, parser.AllErrors)
	if err != nil {
		log.Fatalln(str(err))
	}
	for _, v := range files {
		for fileName, v := range v.Files { // csv/xxx.go
			var importNode *ast.GenDecl
			gg := make([]*GenConfig, 0)

			ast.Inspect(v, func(node ast.Node) bool {
				if i, ok := node.(*ast.GenDecl); ok && i.Tok == token.IMPORT {
					importNode = i
					return true
				}

				if vs, ok := node.(*ast.ValueSpec); ok {
					if len(vs.Names) > 0 {
						mapName := vs.Names[0].Name // map 名字
						if strings.HasSuffix(mapName, "Map") {
							if len(vs.Values) > 0 {
								cl := vs.Values[0].(*ast.CompositeLit)
								if mt, ok := cl.Type.(*ast.MapType); ok {
									if se, ok := mt.Value.(*ast.StarExpr); ok {
										if id, ok := se.X.(*ast.Ident); ok {
											valueName := id.Name // map value的名字
											values := cl.Elts    // map 中的k v

											cl.Elts = nil // 清空map

											gg = append(gg, &GenConfig{
												VarName:    mapName,
												ValVarName: valueName,
												Records:    ConvertToCSV(values),
											})
											return false
										}
									}
								}
							}
						}
					}
				}
				return true
			}) // 遍历文件
			if len(gg) == 0 {
				log.Println("skip ------------->", fileName)
				continue
			}

			AddImport(v, importNode)

			fileBuf := bytes.NewBuffer(make([]byte, 0, 1024*16))

			err = format.Node(fileBuf, token.NewFileSet(), v)
			if err != nil {
				log.Fatalln(str(err))
			}

			for _, v := range gg {
				err = defTpl.Execute(fileBuf, v.VarName)
				if err != nil {
					log.Fatalln(str(err))
				}

				recF, err := os.OpenFile(fmt.Sprintf("csv/output/%s.csv", v.VarName),
					os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
				if err != nil {
					log.Fatalln(str(err))
				}

				err = cc.NewWriter(recF).WriteAll(v.Records)
				if err != nil {
					log.Fatalln(str(err))
				}
				_ = recF.Close()
			}

			err = tpl.Execute(fileBuf, struct {
				FN string
				GG []*GenConfig
			}{
				FN: strings.ReplaceAll(fileName, `\`, "/"),
				GG: gg,
			})
			if err != nil {
				log.Fatalln(str(err))
			}

			f, err := os.OpenFile(fileName, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, os.ModePerm)
			if err != nil {
				log.Fatalln(str(err))
			}
			_, err = io.Copy(f, fileBuf)
			if err != nil {
				log.Fatalln(str(err))
			}
			_ = f.Close()
			log.Println(fileName)
		}
	}
}

type GenConfig struct {
	VarName    string
	ValVarName string
	Records    [][]string
}

func str(err error) string {
	_, _, l, _ := runtime.Caller(1)
	return fmt.Sprintf("line:%d %v", l, err)
}

func ConvertToCSV(elts []ast.Expr) [][]string {
	lines := make([][]string, 0, len(elts))
	for _, v := range elts {
		kv := v.(*ast.KeyValueExpr)

		kk := kv.Key.(*ast.BasicLit).Value
		vv := kv.Value.(*ast.UnaryExpr).X

		elts := vv.(*ast.CompositeLit).Elts

		line := make([]string, 0, len(elts)+1)
		line = append(line, kk)

		for _, v := range elts {
			switch t := v.(type) {
			case *ast.UnaryExpr:
				line = append(line, fmt.Sprintf("-%s", t.X.(*ast.BasicLit).Value))

			case *ast.BasicLit:
				line = append(line, t.Value)

			default:
				panic("")
			}
		}

		lines = append(lines, line)
	}
	return lines
}

func AddImport(f *ast.File, g *ast.GenDecl) {
	if g == nil {
		g = &ast.GenDecl{Specs: []ast.Spec{}, Tok: token.IMPORT}
		old := f.Decls
		f.Decls = append(make([]ast.Decl, 0, len(f.Decls)+1), g)
		f.Decls = append(f.Decls, old...)
	}
	if g.Tok != token.IMPORT {
		return
	}
	g.Specs = append(g.Specs, []ast.Spec{
		&ast.ImportSpec{
			Name: ast.NewIdent("_"),
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"embed"`},
		},
		&ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"encoding/csv"`},
		},
		&ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"fmt"`},
		},
		&ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"reflect"`},
		},
		&ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"strconv"`},
		},
		&ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"strings"`},
		},
	}...)
}

var defTpl = template.Must(template.New("").Parse(`
//go:embed output/{{.}}.csv
var {{.}}Csv string
`))

var tpl = template.Must(template.New("").Parse(`
func init() {
{{- range $x := .GG }}
  {
    tv := reflect.Indirect(reflect.ValueOf({{$x.VarName}}))
	av := reflect.ValueOf({{$x.ValVarName}}{})
	at := av.Type()
	arec, err := csv.NewReader(strings.NewReader({{$x.VarName}}Csv)).ReadAll()
	if err != nil {
		panic(err)
	}
	fn := at.NumField() + 1
	for idx, v := range arec {
		if len(v) != fn {
			panic(fmt.Sprintf("file: {{$.FN}} struct: {{$x.ValVarName}} line:%d fields num:%d not equal csv:%d", idx, fn, len(v)))
		}

		nv := &{{$x.ValVarName}}{}
		anv := reflect.Indirect(reflect.ValueOf(nv))

		for i, j := range v[1:] {
			f := reflect.Indirect(anv.Field(i))
            if !f.CanSet() {
                continue
            }
            switch f.Kind() {
			case reflect.Int:
				x, err := strconv.ParseInt(j, 10, 64)
				if err != nil {
					panic(fmt.Sprintf("file: {{$.FN}} struct {{$x.ValVarName}} line:%d field:%d convert %v", idx, i, err))
				}
				f.SetInt(x)
			case reflect.String:
				f.SetString(j[1 : len(j)-1])

			case reflect.Float64, reflect.Float32:
				x, err := strconv.ParseFloat(j, 64)
				if err != nil {
					panic(fmt.Sprintf("file: {{$.FN}} struct {{$x.ValVarName}} line:%d field:%d convert %v", idx, i, err))
				}
				f.SetFloat(x)
			}
		}

		if strings.HasPrefix(v[0], "` + "`" + `") {
           tv.SetMapIndex(reflect.ValueOf(v[0][1:len(v[0])-1]), reflect.ValueOf(nv))
        } else {
           key, err := strconv.ParseInt(v[0], 10, 64)
           if err != nil {
               panic(fmt.Sprintf("file: {{$.FN}} struct: {{$x.ValVarName}} line:%d convert key %v", idx, err))
           }
           tv.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(nv))
        }
	}
  }
{{end}}
}
`))
