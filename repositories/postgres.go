package repositories

import (
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strconv"

	"github.com/niklucky/vodka"
	"github.com/niklucky/vodka/adapters"
	uuid "github.com/nu7hatch/gouuid"

	lib "github.com/niklucky/go-lib"
)

/*
Postgres - is responsible for storing/fetching data
*/
type Postgres struct {
	adapter            adapters.Adapter
	key                string
	model              interface{}
	source             string
	mapper             Mapper
	debug              bool
	joinedRepositories map[string]joinRepository
}

var defaultParams = make(map[string]interface{})

// getKeyByModel - getting primary key for model to select after create
func getKeyByModel(model interface{}) (key string) {
	st := reflect.ValueOf(model).Elem().Type()
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		if field.Tag.Get("key") != "" {
			key = field.Name
			if field.Tag.Get("db") != "" {
				key = field.Tag.Get("db")
			}
		}
	}
	return
}

func isDebug() (debug bool) {
	if os.Getenv("DEBUG") == "true" {
		return true
	}
	return
}

/*
NewPostgres - Postgres repository recorder
*/
func NewPostgres(adapter adapters.Adapter, source string, model interface{}) Recorder {
	return &Postgres{
		adapter:            adapter,
		key:                getKeyByModel(model),
		source:             source,
		model:              model,
		debug:              isDebug(),
		joinedRepositories: make(map[string]joinRepository),
	}
}

// SetMapper - setting mapper to process data.
// By default will be used base mapper that fills provided Model
// or just will return interface{} with type map[string]interface{}
func (ds *Postgres) SetMapper(m Mapper) {
	ds.mapper = m
}

// Join - joining source to main.
/*
@param joinSource name of source to be joined: JOIN joinSource
@param joinKey - key of joined source to match with main source
@sourceKey - key of main source to join
*/
func (ds *Postgres) Join(joinSource string, joinKey string, sourceKey string, joinType string) {

}

/*
Create - save data to Storage with Adapter
*/
func (ds *Postgres) Create(data interface{}) (interface{}, error) {
	// Checking for auto generated uuid. If found — generating
	uuidx := ds.generateUUID()
	var dataMap map[string]interface{}
	if len(uuidx) > 0 {
		dataMap = data.(map[string]interface{})
		for key, v := range uuidx {
			dataMap[key] = v
		}
		data = dataMap
	}
	// Starting to build INSERT query
	builder := ds.adapter.Builder()
	builder.Insert(ds.source).Values(data)
	SQL := builder.Build()

	if ds.debug {
		fmt.Println("Create SQL: ", SQL)
	}
	result, err := ds.adapter.Exec(SQL)
	if err != nil {
		return nil, err
	}
	// We have auto increment id that is returned
	if id, err := result.LastInsertId(); err == nil {
		return ds.FindByID(id)
	}
	// We have primary key
	if ds.key != "" && dataMap[ds.key] != nil {
		items, err := ds.Find(dataMap, defaultParams)
		if err != nil {
			return data, err
		}
		return items.([]interface{})[0], nil
	}
	// We have nothing, just returning payload back
	return data, nil
}

func (ds *Postgres) generateUUID() (fields map[string]string) {
	fields = make(map[string]string)
	st := reflect.ValueOf(ds.model).Elem().Type()
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		fieldTag := field.Tag.Get("uuid")
		if fieldTag != "" {
			var fieldName = field.Name
			if field.Tag.Get("db") != "" {
				fieldName = field.Tag.Get("db")
			}
			gid, _ := uuid.NewV4()
			fields[fieldName] = gid.String()
		}
	}
	return
}

/*
Delete - deleteing from storage by query
*/
func (ds Postgres) Delete(q QueryMap) (interface{}, error) {
	builder := ds.adapter.Builder()
	SQL := builder.Delete().From(ds.source).Where(q).Build()
	if ds.debug {
		fmt.Println("Delete SQL: ", SQL)
	}

	rows, err := ds.adapter.Exec(SQL)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

/*
DeleteByID - deleteing from storage by query
*/
func (ds *Postgres) DeleteByID(id interface{}) (interface{}, error) {
	builder := ds.adapter.Builder()
	q := make(map[string]interface{})
	q["id"] = id
	SQL := builder.Delete().From(ds.source).Where(q).Build()
	if ds.debug {
		fmt.Println("DeleteByID SQL: ", SQL)
	}
	rows, err := ds.adapter.Exec(SQL)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

/*
Update - updating item in storage by query and payload
*/
func (ds *Postgres) Update(q QueryMap, payload map[string]interface{}) (interface{}, error) {
	builder := ds.adapter.Builder()
	SQL := builder.Update(ds.source).Set(payload).Where(q).Limit(1, 0).Build()
	if ds.debug {
		fmt.Println("Update SQL: ", SQL)
	}
	result, err := ds.adapter.Exec(SQL)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	fmt.Printf("Update Result: %+v\n", id)
	return nil, nil
}

/*
Find - Finding data by query (map key=value) and QueryModificator
Will return Collection
*/
func (ds *Postgres) Find(query QueryMap, params ParamsMap) (interface{}, error) {
	rows, err := ds.fetch(query, params)
	if err != nil {
		return nil, err
	}
	result, err := ds.mapCollection(rows)
	if d, ok := result.([]interface{}); ok {
		if len(d) == 0 {
			return make([]int, 0), err
		}
	}
	return result, err
}

/*
FindByID - fetching Object by id. interface{} because id could be string or int
*/
func (ds *Postgres) FindByID(id interface{}) (interface{}, error) {
	q := make(map[string]interface{})
	q["id"] = id
	data, err := ds.fetch(q, nil)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		return ds.mapItem(data[0])
	}
	return nil, vodka.NewError(404, "not_found", "Item not found")
}

func (ds *Postgres) fetch(query QueryMap, params interface{}) ([]interface{}, error) {
	qb := ds.adapter.Builder()
	var fields []string
	mod := parseParams(params)
	if len(mod.fields) == 0 {
		fields = lib.GetStructTags(reflect.ValueOf(ds.model).Elem(), "db", true)
	} else {
		fields = mod.fields
	}
	if mod.limit == 0 {
		mod.limit = defaultLimit
	}
	qb.Select(fields).
		From(ds.source).
		Where(query).
		Limit(mod.limit, mod.skip)

	// if len(ds.joinedRepositories) > 0 {
	// 	fmt.Printf("Join: %+v\n", ds.joinedRepositories)
	// 	for sourceID, j := range ds.joinedRepositories {
	// 		var on []adapters.JoinParamOn
	// 		if j.condition != nil {
	// 			for key, v := range j.condition {
	// 				on = append(on, adapters.JoinParamOn{
	// 					SourceKey: fmt.Sprintf("%v", v),
	// 					JoinKey:   key,
	// 				})
	// 			}
	// 		}
	// 		if j.conditionValue != nil {
	// 			for key, v := range j.conditionValue {
	// 				on = append(on, adapters.JoinParamOn{
	// 					Source:    j.source,
	// 					SourceKey: key,
	// 					JoinValue: v,
	// 				})
	// 			}
	// 		}
	// 		qb.Join(adapters.JoinParam{
	// 			SourceID: sourceID,
	// 			Source:   j.source,
	// 			Fields:   lib.GetStructTags(j.model, "db", true),
	// 			Type:     j.joinType,
	// 			On:       on,
	// 		})
	// 	}
	// }

	// if len(mod.orderBy) > 0 {
	// 	for _, o := range mod.orderBy {
	// 		qb.Order(o)
	// 	}
	// }

	SQL := qb.Build()
	if ds.debug {
		fmt.Println("Fetch SQL: ", SQL)
	}
	rows, err := ds.adapter.Query(SQL)
	if err != nil {
		fmt.Println("Error: ", err)
		return nil, err
	}
	defer rows.Close()
	return ds.buildResult(rows)
}

func (ds *Postgres) buildResult(rows *sql.Rows) ([]interface{}, error) {
	var result []interface{}
	i := 0
	cols, _ := rows.Columns()
	dest := make([]interface{}, len(cols))
	rawResult := make([]interface{}, len(cols))

	for c := range cols {
		dest[c] = &rawResult[c]
	}

	for rows.Next() {
		data := make(map[string]interface{})
		i++
		if err := rows.Scan(dest...); err != nil {
			fmt.Println("Error: ", err)
			return nil, err
		}
		for key, v := range cols {
			if a, ok := rawResult[key].([]byte); ok == true {
				// data[v] = string(a)
				f, e := strconv.ParseFloat(string(a), 64)
				if e != nil {
					data[v] = string(a)
				} else {
					data[v] = f
				}
			} else {
				data[v] = rawResult[key]
			}
		}
		if ds.model != nil {
			m := reflect.ValueOf(ds.model)
			a := populateStructByMap(m, data)
			result = append(result, a)
		} else {
			result = append(result, data)
		}
	}
	return result, nil
}

func (ds *Postgres) mapCollection(data []interface{}) (interface{}, error) {
	if ds.mapper != nil {
		return ds.mapper.Collection(data)
	}

	return data, nil
}

func (ds *Postgres) mapItem(data interface{}) (interface{}, error) {
	if ds.mapper != nil {
		return ds.mapper.Item(data)
	}
	return data, nil
}

func parseParams(params interface{}) (m QueryModificator) {
	if params == nil {
		return
	}
	if p, ok := params.(map[string]interface{}); ok {
		if p["fields"] != nil {
			m.fields = p["fields"].([]string)
		}
		if p["skip"] != nil {
			m.skip = p["skip"].(int)
		}
		if p["limit"] != nil {
			m.limit = p["limit"].(int)
		}
		// if p["orderBy"] != nil {
		// 	var orderParams adapters.OrderParam
		// 	var orderParamsArr []adapters.OrderParam

		// 	orderParams.OrderBy = p["orderBy"].(string)

		// 	if p["order"] == "asc" {
		// 		orderParams.Asc = true
		// 	} else {
		// 		orderParams.Desc = true
		// 	}

		// 	orderParamsArr = append(orderParamsArr, orderParams)
		// 	m.orderBy = orderParamsArr
		// }
	}
	return
}