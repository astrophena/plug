" Vim syntax file
" Language: PlugScript

if exists("b:current_syntax")
  finish
endif

syntax match purlFunction /\<[[:alpha:]_][[:alnum:]_.]*\ze\s*(/
syntax match purlField /\<[[:alpha:]_][[:alnum:]_]*\ze\s*:/
syntax match purlOperator /=>\|==\|!=\|<=\|>=\|&&\|||\|[+*\/%!=<>|\-]/
syntax match purlDelimiter /[()\[\]{},:;.]/
syntax match purlNumber /\<\d[[:digit:]_]*\%([eE][+-]\=[[:digit:]_]\+\)\=/
syntax match purlNumber /\%([[:digit:]_]*\.[[:digit:]_]\+\|\<[[:digit:]_]\+\.\)\%([eE][+-]\=[[:digit:]_]\+\)\=/
syntax match purlNumber /\<0[xX][[:xdigit:]_]\+\%(\.[[:xdigit:]_]*\)\=\%([pP][+-]\=[[:digit:]_]\+\)\=/
syntax match purlNumber /\<0[bB][01_]\+\|\<0[oO][0-7_]\+/
syntax region purlString start=+"+ skip=+\\.+ end=+"+ oneline
syntax keyword purlKeyword let
syntax keyword purlConstant true false nil null
syntax match purlBuiltin /\.\@<!\<\%(map\|filter\|take\|count\|sortBy\|keys\|parseJSON\|toJSON\|bytes\|text\|find\|any\|all\|contains\|startsWith\|endsWith\|uniqueBy\|groupBy\|sum\|age\|assert\)\>\%(\s*:\)\@!/
syntax match purlComment +//.*$+ contains=@Spell
syntax region purlComment start=+/\*+ end=+\*/+ contains=@Spell
syntax sync minlines=50

highlight default link purlBuiltin Function
highlight default link purlComment Comment
highlight default link purlConstant Constant
highlight default link purlDelimiter Delimiter
highlight default link purlField Identifier
highlight default link purlFunction Function
highlight default link purlKeyword Keyword
highlight default link purlNumber Number
highlight default link purlOperator Operator
highlight default link purlString String

let b:current_syntax = "purl"
