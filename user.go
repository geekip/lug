package main

import (
	"github.com/geekip/lug/util"
	lua "github.com/yuin/gopher-lua"
)

type DB struct {
	util.Module
}

func RegisterUserType(L *lua.LState) {
	L.PreloadModule("user", Loader)
}

func Loader(L *lua.LState) int {
	mod := util.GetModule(L)
	api := util.LGFunctions{"new": new}
	return mod.Api(api)
}

func new(L *lua.LState) int {
	user := &DB{util.GetModule(L)}

	api := util.LGFunctions{
		"setName":  user.SetName,
		"setAge":   user.SetAge,
		"setEmail": user.SetEmail,
	}

	return user.Api(api)
}

// 设置名字
func (u *DB) SetName(L *lua.LState) int {
	println("Name:", L.CheckString(1))
	// u.This(lua.LNumber(1), lua.LNumber(2))
	return u.This()
}

// 设置年龄
func (u *DB) SetAge(L *lua.LState) int {
	println("Age:", L.CheckInt(1))
	return u.This()
}

// 设置邮箱
func (u *DB) SetEmail(L *lua.LState) int {
	println("Email:", L.CheckString(1))
	return u.This()
}

// 打印用户信息
// func (u *User) PrintInfo(L *lua.LState) int {
// 	println("Name:", u.Name)
// 	println("Age:", u.Age)
// 	println("Email:", u.Email)
// 	return 0 // 不需要返回值
// }
