package exec

import (
	"database/sql"
	"fmt"
	"github.com/dk-lockdown/seata-golang/base/mysql"
	"strings"
)

import (
	"github.com/pkg/errors"
)

import (
	"github.com/dk-lockdown/seata-golang/client/at/proxy_tx"
	"github.com/dk-lockdown/seata-golang/client/at/sql/schema"
	"github.com/dk-lockdown/seata-golang/client/at/sql/schema/cache"
	"github.com/dk-lockdown/seata-golang/client/at/sqlparser"
	sql2 "github.com/dk-lockdown/seata-golang/pkg/sql"
)

type InsertExecutor struct {
	proxyTx       *proxy_tx.ProxyTx
	sqlRecognizer sqlparser.ISQLInsertRecognizer
	values        []interface{}
}

func (executor *InsertExecutor) Execute() (sql.Result, error) {
	beforeImage,err := executor.BeforeImage()
	if err != nil {
		return nil,err
	}
	result, err := executor.proxyTx.Exec(executor.sqlRecognizer.GetOriginalSQL(),executor.values...)
	if err != nil {
		return result,err
	}
	afterImage,err := executor.AfterImage()
	if err != nil {
		return nil,err
	}
	executor.PrepareUndoLog(beforeImage,afterImage)
	return result,err
}

func (executor *InsertExecutor) PrepareUndoLog(beforeImage, afterImage *schema.TableRecords) {
	if len(afterImage.Rows)== 0 {
		return
	}

	var lockKeyRecords = afterImage

	lockKeys := buildLockKey(lockKeyRecords)
	executor.proxyTx.AppendLockKey(lockKeys)

	sqlUndoLog := buildUndoItem(executor.sqlRecognizer,beforeImage,afterImage)
	executor.proxyTx.AppendUndoLog(sqlUndoLog)
}

func (executor *InsertExecutor) BeforeImage() (*schema.TableRecords,error) {
	return nil,nil
}

func (executor *InsertExecutor) AfterImage() (*schema.TableRecords,error) {
	pkValues := executor.GetPkValuesByColumn()
	afterImage,err := executor.BuildTableRecords(pkValues)
	if err != nil {
		return nil,err
	}
	return afterImage,nil
}

func (executor *InsertExecutor) BuildTableRecords(pkValues []interface{}) (*schema.TableRecords,error) {
	tableMeta,err := executor.getTableMeta()
	if err != nil {
		return nil,err
	}
	var sb strings.Builder
	fmt.Fprint(&sb,"SELECT ")
	var i = 0
	columnCount := len(tableMeta.AllColumns)
	for _,columnMeta := range tableMeta.AllColumns {
		fmt.Fprint(&sb, mysql.CheckAndReplace(columnMeta.ColumnName))
		i = i + 1
		if i < columnCount {
			fmt.Fprint(&sb,",")
		} else {
			fmt.Fprint(&sb," ")
		}
	}
	fmt.Fprintf(&sb,"FROM %s ", executor.sqlRecognizer.GetTableName())
	fmt.Fprintf(&sb," WHERE `%s` IN ",tableMeta.GetPkName())
	fmt.Fprint(&sb,sql2.AppendInParam(len(pkValues)))

	rows,err := executor.proxyTx.Query(sb.String(),pkValues...)
	if err != nil {
		return nil,errors.WithStack(err)
	}
	return schema.BuildRecords(tableMeta,rows),nil
}

func (executor *InsertExecutor) GetPkValuesByColumn() []interface{} {
	pkValues := make([]interface{},0)
	columnLen := executor.GetColumnLen()
	pkIndex := executor.GetPkIndex()
	for i,value := range executor.values {
		if i % columnLen ==  pkIndex {
			pkValues = append(pkValues,value)
		}
	}
	return pkValues
}

func(executor *InsertExecutor) GetPkIndex() int {
	insertColumns := executor.sqlRecognizer.GetInsertColumns()
	tableMeta,_ := executor.getTableMeta()

	if insertColumns != nil && len(insertColumns)>0 {
		for i,columnName := range insertColumns {
			if tableMeta.GetPkName() == columnName {
				return i
			}
		}
	}
	allColumns := tableMeta.AllColumns
	var idx = 0
	for _,column := range allColumns {
		if tableMeta.GetPkName() == column.ColumnName {
			return idx
		}
		idx = idx + 1
	}
	return 0
}

func (executor *InsertExecutor) GetColumnLen() int {
	insertColumns := executor.sqlRecognizer.GetInsertColumns()
	if insertColumns != nil {
		return len(insertColumns)
	}
	tableMeta,_ := cache.GetTableMetaCache().GetTableMeta(executor.proxyTx.Tx,
		executor.sqlRecognizer.GetTableName(),
		executor.proxyTx.ResourceId)

	return len(tableMeta.AllColumns)
}

func (executor *InsertExecutor) getTableMeta() (schema.TableMeta,error) {
	tableMetaCache := cache.GetTableMetaCache()
	return tableMetaCache.GetTableMeta(executor.proxyTx.Tx,executor.sqlRecognizer.GetTableName(),executor.proxyTx.ResourceId)
}