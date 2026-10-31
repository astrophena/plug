" Vim syntax file
" Language: Plug schema

if exists("b:current_syntax")
  finish
endif

syntax match plugConstructor /^\s*\zs[A-Za-z_][A-Za-z0-9_]*\ze\%([[:space:]]\|$\)/
syntax match plugMethod /\<[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*\>/
syntax match plugType /\<[A-Z][A-Za-z0-9_]*\>/
syntax match plugOperator /->\|[=?:<>]/
syntax match plugTerminator /;/
syntax match plugNumber /\<\d\+\>/
syntax region plugString start=+"+ skip=+\\"+ end=+"+
syntax keyword plugKeyword category http httpStatus package permission public requires throws type version
syntax keyword plugPrimitive bool bytes f64 i32 i64 raw string u32 u64 void
syntax region plugComment start=+//+ end=+$+ contains=@Spell

highlight default link plugComment Comment
highlight default link plugConstructor Function
highlight default link plugKeyword Keyword
highlight default link plugMethod Function
highlight default link plugNumber Number
highlight default link plugOperator Operator
highlight default link plugPrimitive Type
highlight default link plugString String
highlight default link plugTerminator Delimiter
highlight default link plugType Type

let b:current_syntax = "plug"
