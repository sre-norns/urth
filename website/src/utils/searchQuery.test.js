import assert from "assert";

import { SearchQuery } from "./searchQuery";

assert.deepEqual(new SearchQuery(), {
    matchLabels: {
        labelA: "a"
    },
    matchExpressions: [{
        operator: "NotIn",
        key: "labelB",
        values: ["b"]
    }]
});