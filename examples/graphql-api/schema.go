package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/graphql-go/graphql"
)

// Order is the GraphQL Order type backing store.
type Order struct {
	ID          string  `json:"id"`
	Amount      int     `json:"amount"`
	Currency    string  `json:"currency"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
}

var (
	storeMu sync.Mutex
	orders  = make(map[string]*Order)
	seq     int64
)

func nextOrderID() string {
	seq++
	return fmt.Sprintf("ord-%04d", seq)
}

func buildSchema() (graphql.Schema, error) {
	orderStatusEnum := graphql.NewEnum(graphql.EnumConfig{
		Name: "OrderStatus",
		Values: graphql.EnumValueConfigMap{
			"PENDING":   &graphql.EnumValueConfig{Value: "PENDING"},
			"CONFIRMED": &graphql.EnumValueConfig{Value: "CONFIRMED"},
			"SHIPPED":   &graphql.EnumValueConfig{Value: "SHIPPED"},
			"DELIVERED": &graphql.EnumValueConfig{Value: "DELIVERED"},
			"CANCELLED": &graphql.EnumValueConfig{Value: "CANCELLED"},
		},
	})

	createOrderInput := graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "CreateOrderInput",
		Fields: graphql.InputObjectConfigFieldMap{
			"amount":      &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.Int)},
			"currency":    &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
			"description": &graphql.InputObjectFieldConfig{Type: graphql.String},
		},
	})

	orderType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Order",
		Fields: graphql.FieldsThunk(func() graphql.Fields {
			return graphql.Fields{
				"id": &graphql.Field{
					Type: graphql.NewNonNull(graphql.ID),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						return p.Source.(*Order).ID, nil
					},
				},
				"amount": &graphql.Field{
					Type: graphql.NewNonNull(graphql.Int),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						return p.Source.(*Order).Amount, nil
					},
				},
				"currency": &graphql.Field{
					Type: graphql.NewNonNull(graphql.String),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						return p.Source.(*Order).Currency, nil
					},
				},
				"description": &graphql.Field{
					Type: graphql.String,
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						return p.Source.(*Order).Description, nil
					},
				},
				"status": &graphql.Field{
					Type: graphql.NewNonNull(orderStatusEnum),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						return p.Source.(*Order).Status, nil
					},
				},
				"createdAt": &graphql.Field{
					Type: graphql.NewNonNull(graphql.String),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						return p.Source.(*Order).CreatedAt, nil
					},
				},
			}
		}),
	})

	rootQuery := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"health": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
				Resolve: func(graphql.ResolveParams) (interface{}, error) {
					return "ok", nil
				},
			},
			"orders": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(orderType))),
				Args: graphql.FieldConfigArgument{
					"status": &graphql.ArgumentConfig{Type: orderStatusEnum},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					want, has := p.Args["status"].(string)
					storeMu.Lock()
					defer storeMu.Unlock()
					out := make([]*Order, 0, len(orders))
					for _, o := range orders {
						if has && want != "" && o.Status != want {
							continue
						}
						out = append(out, o)
					}
					return out, nil
				},
			},
			"order": &graphql.Field{
				Type: orderType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					id, _ := p.Args["id"].(string)
					storeMu.Lock()
					defer storeMu.Unlock()
					return orders[id], nil
				},
			},
		},
	})

	rootMutation := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			"createOrder": &graphql.Field{
				Type: graphql.NewNonNull(orderType),
				Args: graphql.FieldConfigArgument{
					"input": &graphql.ArgumentConfig{Type: graphql.NewNonNull(createOrderInput)},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					in, _ := p.Args["input"].(map[string]interface{})
					amt, _ := in["amount"].(int)
					if f, ok := in["amount"].(float64); ok {
						amt = int(f)
					}
					cur, _ := in["currency"].(string)
					var desc *string
					if d, ok := in["description"].(string); ok && d != "" {
						desc = &d
					}
					storeMu.Lock()
					defer storeMu.Unlock()
					id := nextOrderID()
					o := &Order{
						ID:          id,
						Amount:      amt,
						Currency:    cur,
						Description: desc,
						Status:      "PENDING",
						CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
					}
					orders[id] = o
					return o, nil
				},
			},
			"updateOrderStatus": &graphql.Field{
				Type: graphql.NewNonNull(orderType),
				Args: graphql.FieldConfigArgument{
					"id":     &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"status": &graphql.ArgumentConfig{Type: graphql.NewNonNull(orderStatusEnum)},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					id, _ := p.Args["id"].(string)
					st, _ := p.Args["status"].(string)
					storeMu.Lock()
					defer storeMu.Unlock()
					o, ok := orders[id]
					if !ok {
						return nil, fmt.Errorf("order not found")
					}
					o.Status = st
					return o, nil
				},
			},
		},
	})

	return graphql.NewSchema(graphql.SchemaConfig{
		Query:    rootQuery,
		Mutation: rootMutation,
	})
}

func corruptAmountBug(data interface{}) {
	if os.Getenv("BT_GQL_AMOUNT_BUG") != "1" {
		return
	}
	switch x := data.(type) {
	case map[string]interface{}:
		if v, ok := x["amount"]; ok && isIntegral(v) {
			x["amount"] = "one hundred"
		}
		for _, v := range x {
			corruptAmountBug(v)
		}
	case []interface{}:
		for _, v := range x {
			corruptAmountBug(v)
		}
	}
}

func isIntegral(v interface{}) bool {
	switch v.(type) {
	case int, int32, int64, uint, uint32, uint64, float64:
		return true
	default:
		return false
	}
}
