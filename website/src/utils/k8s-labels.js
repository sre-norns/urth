
const hasOwnProperty = Function.call.bind(Object.prototype.hasOwnProperty);

const Operator = {
    Exists: "Exists",
    DoesNotExist: "DoesNotExist",
    Equals: "=",
    NotEquals: "!=",
    In: "In",
    NotIn: "NotIn",

    GreaterThen: ">",
    LessThen: "<"
}

class Rule {
    constructor(key, op, values) {
        this.key = key;
        this.operator = op;
        this.values = (values || [])
    }

    toString() {
        switch (this.operator) {
            case Operator.Exists: return this.key;
            case Operator.DoesNotExist: return `!${this.key}`;
            case Operator.Equals: return `${this.key} = ${this.values[0]}`;
            case Operator.NotEquals: return `${this.key} != ${this.values[0]}`;
            case Operator.GreaterThen: return `${this.key} > ${this.values[0]}`;
            case Operator.LessThen: return `${this.key} < ${this.values[0]}`;
            case Operator.In: return `${this.key} in (${this.values.join(",")})`;
            case Operator.NotIn: return `${this.key} notin (${this.values.join(",")})`;
            default:
                return `${this.key} ${this.operator} (${this.values.join(",")})`;
        }
    }
}

function parseExpression(expr) {
    const parts = expr.trim().split(" ");
    const key = parts[0];
    const operator = parts[1];
    const value = parts[2];
    const values = (value || "")
        .slice(1, -1)
        .split(",")
        .map(val => val.trim());

    switch (operator) {
        case undefined:
            return key[0] === "!"
                ? new Rule(key.slice(1), Operator.DoesNotExist)
                : new Rule(key, Operator.Exists);
        case "=":
        case "==": return new Rule(key, Operator.Equals, [value]);
        case "!=": return new Rule(key, Operator.NotEquals, [value]);
        case "in": return new Rule(key, Operator.In, values);
        case "notin": return new Rule(key, Operator.NotIn, values);
        case ">": return new Rule(key, Operator.GreaterThen, [value]);
        case "<": return new Rule(key, Operator.LessThen, [value]);

        default:
    }

    throw new Error(`Invalid expression: ${expr}`);
}

function parseSelector(labelSelector) {
    const expressions = labelSelector
        .split(/,(?![^(]*\))/)
        .map(parseExpression);

    const matchLabels = expressions
        .filter(expr => hasOwnProperty(expr, "value"))
        .reduce((labels, expr) => Object.assign(labels, {
            [expr.key]: expr.value
        }), {});

    const matchExpressions = expressions
        .filter(expr => !hasOwnProperty(expr, "value"));

    return { matchLabels, matchExpressions };
}

function parseSelectorExpression(labelSelector) {
    return labelSelector
        .split(/,(?![^(]*\))/)
        .map(parseExpression);
}


function stringifyExpression(expr) {
    const operator = expr.operator.toLowerCase();
    const key = expr.key;
    const values = expr.values;

    switch (operator) {
        case "exists": return key;
        case "doesnotexist": return `!${key}`;
        default:
    }

    return `${key} ${operator} (${values.join(",")})`;
}

function stringify(rules) {
    return rules
        .map(rule => rule.toString())
        .join(",");
}


function stringifySelector(opts) {
    const matchLabels = opts.matchLabels || {};
    const matchExpressions = opts.matchExpressions || [];

    return Object
        .keys(matchLabels)
        .map(key => `${key} = ${matchLabels[key]}`)
        .concat(matchExpressions.map(stringifyExpression))
        .join(",");
}

function getMatchExpressions(opts) {
    if (typeof opts === "string") {
        return getMatchExpressions(parse(opts));
    }

    const matchExpressions = opts.matchExpressions || [];
    const matchLabels = opts.matchLabels || {};

    return Object
        .keys(matchLabels)
        .map(label => ({
            operator: "In",
            key: label,
            values: [matchLabels[label]]
        }))
        .concat(matchExpressions);
}

function isExprMatch(expr, labels) {
    const op = expr.operator;
    const key = expr.key;
    const values = expr.values;
    const label = labels[key];

    switch (op) {
        case "Exists": return hasOwnProperty(labels, key);
        case "DoesNotExist": return !hasOwnProperty(labels, key);
        case "In": return values.indexOf(label) >= 0;
        case "NotIn": return values.indexOf(label) < 0;
        default:
    }

    throw new Error(`Invalid operator: ${op}`);
}

function Selector(opts) {
    const expressions = getMatchExpressions(opts);

    return labels => expressions
        .every(expr => isExprMatch(expr, labels || {}));
}

export {
    Rule,
    Operator,
    stringify,
    parseSelector,
    parseSelectorExpression,
    Selector
};