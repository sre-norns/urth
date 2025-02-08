import assert from "assert";

import { parse } from "./k8s-labels";

assert.deepEqual(parse("labelA = a, labelB != b"), {
    matchLabels: {
        labelA: "a"
    },
    matchExpressions: [{
        operator: "NotIn",
        key: "labelB",
        values: ["b"]
    }]
});