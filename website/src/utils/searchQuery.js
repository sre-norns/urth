import { parseSelectorExpression, stringify } from './k8s-labels.js'

class SearchQuery {
    constructor(urlQueryParams) {
        this.sourceQuery = urlQueryParams || new URLSearchParams();
        this.name = urlQueryParams.get("name")

        const page = this.sourceQuery.get("page")
        if (page) {
            this.page = parseInt(page, 10);
        }

        const pageSize = this.sourceQuery.get("pageSize")
        if (pageSize) {
            try {
                this.pageSize = parseInt(pageSize, 10);
            } catch (error) {

            }
        }

        this.labels = this.sourceQuery.get("labels") || ""
    }

    setRule(rule) {
        if (!this._expressions) {
            this._expressions = [rule]
        } else {
            this._expressions.push(rule)
        }
    }

    get labels() {
        return (this._expressions)
            ? stringify(this._expressions)
            : this.sourceQuery.get("labels") || ""
    }

    set labels(value) {
        if (value) {
            try {
                console.log(`parsing labels value "${value}" into a selector expr`)
                this._expressions = parseSelectorExpression(value)
                console.log(`exprs `, this._expressions)
            } catch (error) {
                console.log(`Failed to parse query labels "${value}" into a selector expr: `, error)
            }
        } else {
            this._expressions = null
        }
    }

    get urlSearchParams() {
        const urlQueryParams = this.sourceQuery
        if (this.name) {
            urlQueryParams.set("name", this.name)
        } else {
            urlQueryParams.delete("name")
        }
        if (this.page) {
            urlQueryParams.set("page", this.page)
        } else {
            urlQueryParams.delete("page")
        }
        if (this.pageSize) {
            urlQueryParams.set("pageSize", this.pageSize)
        } else {
            urlQueryParams.delete("pageSize")
        }

        if (this._expressions) {
            const expr = stringify(this._expressions)
            console.log(`setting labels value from expr "${expr}"`)
            urlQueryParams.set("labels", expr)
        } else {
            urlQueryParams.delete("labels")
        }

        return urlQueryParams
    }

    toString() {
        return urlSearchParams.toString()
    }
}

export {
    SearchQuery
};