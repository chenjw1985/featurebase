import React, { FC, useState } from 'react';
import moment, { Moment } from 'moment';
import uniqBy from 'lodash/uniqBy';
import { Query } from './Query';
import { pilosa } from 'services/eventServices';
import { useEffectOnce } from 'react-use';
import { grpc } from '@improbable-eng/grpc-web';
import { queryPQL, querySQL } from 'services/grpcServices';
import { ColumnInfo, ColumnResponse, RowResponse } from 'proto/pilosa_pb';

export type ResultType = {
  query: string;
  operation: string;
  type: 'PQL' | 'SQL';
  headers: ColumnInfo.AsObject[];
  rows: ColumnResponse.AsObject[][];
  duration?: number;
  roundtrip: number;
  index?: string;
  error: string;
  totalMessageCount: number;
};

let streamingResults: ResultType = {
  query: '',
  operation: '',
  type: 'SQL',
  headers: [],
  rows: [],
  roundtrip: 0,
  error: '',
  totalMessageCount: 0,
};

export const QueryContainer: FC<{}> = () => {
  let startTime: Moment;

  const MAX_MESSAGES = 1000; // same limit as in query builder

  const [indexes, setIndexes] = useState<any>();
  const [results, setResults] = useState<ResultType[]>([]);
  const [errorResult, setErrorResult] = useState<ResultType>();
  const [loading, setLoading] = useState<boolean>(false);

  useEffectOnce(() => {
    pilosa.get.schema().then((res) => {
      setIndexes(res.data.indexes);
    });
  });

  const handleQueryMessages = (message: RowResponse) => {
    if (streamingResults.totalMessageCount < MAX_MESSAGES) {
      const response = message.toObject();
      if (response.headersList.length > 0) {
        streamingResults.headers = response.headersList;
        streamingResults.duration = response.duration;
      }
      streamingResults.rows.push(response.columnsList);
    }
    streamingResults.totalMessageCount += 1;
  };

  const handleQueryEnd = (status: grpc.Code, statusMessage: string) => {
    if (status !== grpc.Code.OK) {
      streamingResults.error = statusMessage;
      setErrorResult(streamingResults);
    } else {
      let recentQueries = JSON.parse(
        localStorage.getItem('recent-queries') || '[]'
      );
      const lastQuery = localStorage.getItem('last-query');
      recentQueries.unshift(lastQuery);
      recentQueries = uniqBy(recentQueries);

      if (recentQueries.length > 10) {
        localStorage.setItem(
          'recent-queries',
          JSON.stringify(recentQueries.slice(0, 9))
        );
      } else {
        localStorage.setItem('recent-queries', JSON.stringify(recentQueries));
      }

      streamingResults.roundtrip = moment
        .duration(moment().diff(startTime))
        .as('milliseconds');
      setErrorResult(undefined);
      setResults([streamingResults, ...results]);
    }
    setLoading(false);
  };

  const onQuery = (query: string, type: 'PQL' | 'SQL', index?: string) => {
    streamingResults = {
      query,
      operation: '',
      type,
      headers: [],
      rows: [],
      index,
      roundtrip: 0,
      error: '',
      totalMessageCount: 0,
    };
    startTime = moment();
    if (query) {
      setLoading(true);
      localStorage.setItem(
        'last-query',
        type === 'PQL' ? `[${index}]${query}` : query
      );

      if (type === 'PQL') {
        if (index) {
          queryPQL(index, query, handleQueryMessages, handleQueryEnd);
        } else {
          streamingResults.error = 'missing index';
          setErrorResult(streamingResults);
          setLoading(false);
        }
      } else {
        querySQL(query, handleQueryMessages, handleQueryEnd);
      }
    }
  };

  const removeResultItem = (resultIdx: number) => {
    const resultsClone = [...results];
    resultsClone.splice(resultIdx, 1);
    setResults(resultsClone);
  };

  return (
    <Query
      indexList={indexes}
      onQuery={onQuery}
      results={results}
      error={errorResult}
      loading={loading}
      onClear={() => setResults([])}
      onRemoveResult={removeResultItem}
    />
  );
};
