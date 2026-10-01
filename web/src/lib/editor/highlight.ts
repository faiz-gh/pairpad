import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags as t } from '@lezer/highlight';

/**
 * Syntax colors for every theme. Each rule points at a CSS variable defined
 * in routes/layout.css for light and dark mode, so switching themes never
 * reconfigures the editor. Only loaded together with a language grammar.
 */
const style = HighlightStyle.define([
	{
		tag: [t.keyword, t.modifier, t.controlKeyword, t.operatorKeyword],
		color: 'var(--syntax-keyword)'
	},
	{ tag: [t.string, t.special(t.string), t.regexp, t.character], color: 'var(--syntax-string)' },
	{
		tag: [t.comment, t.lineComment, t.blockComment, t.docComment],
		color: 'var(--syntax-comment)',
		fontStyle: 'italic'
	},
	{ tag: [t.number, t.bool, t.null, t.atom, t.literal], color: 'var(--syntax-number)' },
	{
		tag: [t.function(t.variableName), t.function(t.propertyName), t.macroName],
		color: 'var(--syntax-function)'
	},
	{
		tag: [t.typeName, t.className, t.namespace, t.standard(t.typeName)],
		color: 'var(--syntax-type)'
	},
	{ tag: [t.propertyName, t.definition(t.propertyName)], color: 'var(--syntax-property)' },
	{ tag: [t.tagName, t.angleBracket], color: 'var(--syntax-tag)' },
	{ tag: [t.attributeName], color: 'var(--syntax-attribute)' },
	{ tag: [t.meta, t.processingInstruction, t.annotation], color: 'var(--syntax-meta)' },
	{ tag: [t.heading], fontWeight: 'bold', color: 'var(--syntax-keyword)' },
	{ tag: [t.emphasis], fontStyle: 'italic' },
	{ tag: [t.strong], fontWeight: 'bold' },
	{ tag: [t.link, t.url], color: 'var(--syntax-string)', textDecoration: 'underline' },
	{ tag: [t.invalid], color: 'var(--syntax-invalid)' }
]);

export const highlighting = syntaxHighlighting(style);
