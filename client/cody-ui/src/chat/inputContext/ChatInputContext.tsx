import React, { useMemo } from 'react'

import { mdiFileDocumentOutline, mdiSourceRepository, mdiFileExcel } from '@mdi/js'
import classNames from 'classnames'

import { ChatContextStatus } from '@sourcegraph/cody-shared/src/chat/context'
import { basename, isDefined } from '@sourcegraph/common'

import { Icon } from '../../utils/Icon'

import styles from './ChatInputContext.module.css'

const infoMsg =
    "This codebase does not have embeddings. To enable Cody's full capabilities, configure embeddings in your Sourcegraph instance. See our docs on how to configure embeddings at https://docs.sourcegraph.com/cody/explanations/code_graph_context#embeddings"

export const ChatInputContext: React.FunctionComponent<{
    contextStatus: ChatContextStatus
    className?: string
}> = ({ contextStatus, className }) => {
    const items: Pick<React.ComponentProps<typeof ContextItem>, 'icon' | 'text' | 'tooltip'>[] = useMemo(
        () =>
            [
                contextStatus.codebase
                    ? {
                          icon: contextStatus.connection ? mdiSourceRepository : mdiFileExcel,
                          text: basename(contextStatus.codebase.replace(/^(github|gitlab)\.com\//, '')),
                          tooltip: contextStatus.connection ? contextStatus.codebase : infoMsg,
                      }
                    : null,
                contextStatus.filePath
                    ? {
                          icon: mdiFileDocumentOutline,
                          text: basename(contextStatus.filePath),
                          tooltip: contextStatus.filePath,
                      }
                    : null,
            ].filter(isDefined),
        [contextStatus.codebase, contextStatus.connection, contextStatus.filePath]
    )

    return (
        <div className={classNames(styles.container, className)}>
            {contextStatus.mode && contextStatus.connection ? (
                <h3 title="Current Context: Embedded" className={styles.badge}>
                    Embeddings
                </h3>
            ) : contextStatus.supportsKeyword ? (
                <h3 title="Current Context: Local Keyword" className={styles.badge}>
                    Keyword
                </h3>
            ) : null}

            {items.length > 0 && (
                <ul className={styles.items}>
                    {items.map(({ icon, text, tooltip }, index) => (
                        // eslint-disable-next-line react/no-array-index-key
                        <ContextItem key={index} icon={icon} text={text} tooltip={tooltip} as="li" />
                    ))}
                </ul>
            )}
        </div>
    )
}

const ContextItem: React.FunctionComponent<{ icon: string; text: string; tooltip?: string; as: 'li' }> = ({
    icon,
    text,
    tooltip,
    as: Tag,
}) => (
    <Tag className={tooltip === infoMsg ? styles.info : styles.item}>
        <Icon svgPath={icon} className={styles.itemIcon} />
        <span className={styles.itemText} title={tooltip}>
            {text}
        </span>
    </Tag>
)
