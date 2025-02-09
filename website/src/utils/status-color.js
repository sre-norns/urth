

const statusToColor = (status) => {
    switch (status?.status) {
        case 'pending':
            return 'warning'
        case 'running':
            return 'primary'
        case 'timeout':
            return 'secondary'
        case 'errored':
            return 'warning'
        case 'completed':
            switch (status.result) {
                case 'success':
                    return 'success'
                case 'failed':
                    return 'error'
                case 'errored':
                    return 'warning'
                case 'canceled':
                    return 'neutral'
                case 'timeout':
                    return 'error'
                default:
                    return 'neutral'
            }
        default:
            return 'neutral'
    }
}

export { statusToColor }